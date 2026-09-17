// Package tunnel runs the developer's cloudflared for orb dev (ADR-0086):
// a quick tunnel (a random *.trycloudflare.com URL, no account) or a named
// tunnel (the developer's hostname, with the token from .env), exposing the
// app, and only the app, on a public HTTPS address.
//
// orb dev owns the cloudflared process: it starts in a process group of its
// own and is stopped with its group when orb dev stops, when the Dev Portal
// asks, or before a restart. The token travels to cloudflared in its
// environment (TUNNEL_TOKEN), never on its command line, and is removed from
// every line of cloudflared's output orb keeps or prints; no status or error
// this package returns contains it.
package tunnel

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Mode is how the tunnel gets its public address.
type Mode string

// Modes.
const (
	// ModeQuick is a quick tunnel: a random trycloudflare.com URL that
	// changes on every start, without a Cloudflare account.
	ModeQuick Mode = "quick"
	// ModeNamed is a tunnel created in the Cloudflare dashboard, with the
	// developer's hostname and token.
	ModeNamed Mode = "named"
)

// ParseMode returns the mode named s.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeQuick, ModeNamed:
		return Mode(s), nil
	}
	return "", refusal(CodeInvalidMode, "unknown tunnel mode %q: use quick or named", truncate(s, 40))
}

// State is what the tunnel is doing.
type State string

// States.
const (
	StateOff        State = "off"
	StateStarting   State = "starting"
	StateConnected  State = "connected"
	StateStopping   State = "stopping"
	StateFailed     State = "failed"
	redactedMessage       = "[redacted]"
)

// LogLine is one line of cloudflared's output, with secrets removed.
type LogLine struct {
	Time time.Time `json:"time"`
	// Level is cloudflared's: DBG, INF, WRN, ERR, FTL, or "".
	Level string `json:"level,omitempty"`
	Text  string `json:"text"`
}

// Reachability is the result of requesting the public URL's /livez.
type Reachability struct {
	URL       string    `json:"url"`
	OK        bool      `json:"ok"`
	Status    int       `json:"status,omitempty"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checked_at"`
	LatencyMS float64   `json:"latency_ms"`
}

// Status describes the tunnel.
type Status struct {
	State State `json:"state"`
	Mode  Mode  `json:"mode,omitempty"`
	// PublicURL is https://<hostname>: a quick tunnel's once cloudflared
	// reports it, a named tunnel's from the start.
	PublicURL string `json:"public_url,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	// Stable reports a URL that stays the same across runs (named
	// tunnels): only such a URL suits sign-in callbacks and passkeys.
	Stable bool `json:"stable"`
	// Target is the app's address cloudflared forwards to (a quick
	// tunnel's --url; for a named tunnel, where its public hostname
	// should point in the dashboard).
	Target      string     `json:"target,omitempty"`
	PID         int        `json:"pid,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	ConnectedAt *time.Time `json:"connected_at,omitempty"`
	// Problem says why the tunnel failed, or what needs attention.
	Problem string `json:"problem,omitempty"`
	// Restarts counts starts after the first in this orb dev run.
	Restarts int `json:"restarts"`
	// Check is the last reachability check.
	Check *Reachability `json:"check,omitempty"`
	// Log holds cloudflared's most recent lines, oldest first.
	Log []LogLine `json:"log"`
}

// StartOptions choose the tunnel to start.
type StartOptions struct {
	Mode Mode `json:"mode"`
	// Hostname is a named tunnel's public hostname; empty uses
	// ORB_TUNNEL_HOSTNAME, then the one saved from the portal. A hostname
	// given here is saved for the next run unless ORB_TUNNEL_HOSTNAME is set.
	Hostname string `json:"hostname,omitempty"`
}

// Config configures a [Manager].
type Config struct {
	// Dir is the app directory (for SettingsFile and cloudflared's working
	// directory).
	Dir string
	// Env returns orb dev's view of the environment: the process
	// environment and .env, the environment winning. It is read on every
	// start, for APP_ENV, the token, the hostname and ORB_CLOUDFLARED.
	Env func() []string
	// Target returns the app's address as reachable from this machine,
	// such as http://127.0.0.1:8080.
	Target func() (string, error)
	// OnChange receives the status after every change; it must not block.
	OnChange func(Status)
	// Logf prints orb's own messages about the tunnel; nil discards them.
	Logf func(format string, args ...any)
	// LookPath finds programs; nil uses exec.LookPath.
	LookPath func(string) (string, error)
	// ProcessEnv is the environment cloudflared inherits (without any
	// tunnel token variables); nil uses os.Environ.
	ProcessEnv func() []string
	// StartTimeout bounds how long cloudflared may take to connect (and a
	// quick tunnel to report its URL); zero means 45 seconds.
	StartTimeout time.Duration
	// StopTimeout is how long cloudflared gets to exit after SIGTERM
	// before its group is killed; zero means 5 seconds.
	StopTimeout time.Duration
	// Client makes reachability checks; nil uses a client with a 10 second
	// timeout. Redirects are never followed.
	Client *http.Client
	// CheckAttempts and CheckInterval shape the check after connecting,
	// which retries while DNS and Cloudflare catch up; zero means 6 and 5
	// seconds, and negative attempts turn the automatic check off.
	CheckAttempts int
	CheckInterval time.Duration
}

// maxLogLines is how many lines of cloudflared's output Status keeps.
const maxLogLines = 100

// Manager starts, stops and watches cloudflared. It is safe for concurrent
// use.
type Manager struct {
	cfg Config

	// startMu serialises Start, Stop and Restart.
	startMu sync.Mutex

	mu      sync.Mutex
	status  Status
	run     *process
	opts    StartOptions
	started bool
	closed  bool
	// versions caches cloudflared --version per path.
	versions map[string]string
}

// process is one cloudflared run.
type process struct {
	cmd      *exec.Cmd
	done     chan struct{}
	secrets  []string
	stopping bool
	lastErr  string
}

// New returns a manager; nothing runs until Start.
func New(cfg Config) *Manager {
	if cfg.Env == nil {
		cfg.Env = os.Environ
	}
	if cfg.ProcessEnv == nil {
		cfg.ProcessEnv = os.Environ
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.OnChange == nil {
		cfg.OnChange = func(Status) {}
	}
	if cfg.LookPath == nil {
		cfg.LookPath = exec.LookPath
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 45 * time.Second
	}
	if cfg.StopTimeout <= 0 {
		cfg.StopTimeout = 5 * time.Second
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	client := *cfg.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	cfg.Client = &client
	if cfg.CheckAttempts == 0 {
		cfg.CheckAttempts = 6
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 5 * time.Second
	}
	return &Manager{cfg: cfg, status: Status{State: StateOff, Log: []LogLine{}}, versions: map[string]string{}}
}

// Status returns the tunnel's status.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshot()
}

// snapshot copies the status; the caller holds mu.
func (m *Manager) snapshot() Status {
	s := m.status
	s.Log = append([]LogLine{}, m.status.Log...)
	if s.Check != nil {
		c := *s.Check
		s.Check = &c
	}
	return s
}

// publish hands the status to OnChange; the caller holds mu.
func (m *Manager) publish() { m.cfg.OnChange(m.snapshot()) }

// Start starts a tunnel, stopping a running one first. It returns once
// cloudflared runs; its URL and connection arrive as status changes. It
// refuses, with an [*Error], when cloudflared is missing, APP_ENV isn't
// development, the app's address is unknown, or a named tunnel lacks its
// hostname or a valid token.
func (m *Manager) Start(ctx context.Context, opts StartOptions) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	return m.start(ctx, opts)
}

func (m *Manager) start(_ context.Context, opts StartOptions) error {
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return errors.New("orb dev is stopping")
	}
	mode, err := ParseMode(string(opts.Mode))
	if err != nil {
		return err
	}
	env := m.cfg.Env()
	if appEnv := envValue(env, "APP_ENV"); appEnv != "" && appEnv != "development" {
		return refusal(CodeNotDevelopment, "APP_ENV is %s: orb dev starts a tunnel only for development, because it puts the app on the internet", truncate(appEnv, 40))
	}
	path, err := m.lookBinary(env)
	if err != nil {
		return err
	}
	target, err := m.cfg.Target()
	if err != nil || target == "" {
		return refusal(CodeNoTarget, "the app's address is unknown (APP_ADDR): %v", err)
	}

	var args []string
	var token, hostname string
	switch mode {
	case ModeQuick:
		args = []string{"tunnel", "--url", target, "--no-autoupdate"}
	case ModeNamed:
		if hostname, err = m.hostname(env, opts.Hostname); err != nil {
			return err
		}
		if token, _, err = LoadToken(env); err != nil {
			return err
		}
		args = []string{"tunnel", "--no-autoupdate", "run"}
	}

	if err := m.stop(); err != nil {
		return err
	}
	cmd := exec.Command(path, args...) //nolint:gosec // the developer's own cloudflared, with fixed arguments
	cmd.Dir = m.cfg.Dir
	cmd.Env = childEnv(m.cfg.ProcessEnv(), token)
	cmd.Stdin = nil
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = writer, writer
	ownProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return fmt.Errorf("start cloudflared: %w", err)
	}
	_ = writer.Close()

	p := &process{cmd: cmd, done: make(chan struct{}), secrets: secretsOf(token)}
	now := time.Now().UTC()
	m.mu.Lock()
	restarts := m.status.Restarts
	if m.started {
		restarts++
	}
	m.started, m.run, m.opts = true, p, StartOptions{Mode: mode, Hostname: hostname}
	m.status = Status{
		State: StateStarting, Mode: mode, Target: target, PID: cmd.Process.Pid, StartedAt: &now,
		Restarts: restarts, Stable: mode == ModeNamed, Hostname: hostname, Log: []LogLine{},
	}
	if mode == ModeNamed {
		m.status.PublicURL = "https://" + hostname
	}
	m.publish()
	m.mu.Unlock()

	switch mode {
	case ModeQuick:
		m.cfg.Logf("orb: starting a quick tunnel to %s (cloudflared; the URL follows)", target)
	case ModeNamed:
		m.cfg.Logf("orb: starting the named tunnel for https://%s (cloudflared; its public hostname should point at %s)", hostname, target)
	}

	lines := make(chan struct{})
	go func() {
		defer close(lines)
		m.readLines(p, reader)
	}()
	go func() {
		err := cmd.Wait()
		// Whatever cloudflared started in its group goes with it, also
		// when it exits by itself; then its output ends.
		_ = kill(cmd)
		<-lines
		m.exited(p, err)
	}()
	go m.watchStart(p)
	return nil
}

// lookBinary resolves cloudflared or refuses with the install help.
func (m *Manager) lookBinary(env []string) (string, error) {
	name := "cloudflared"
	if v := envValue(env, BinaryVar); v != "" {
		name = v
	}
	path, err := m.cfg.LookPath(name)
	if err != nil {
		if name != "cloudflared" {
			return "", refusal(CodeCloudflaredMissing, "%s=%s: %v", BinaryVar, truncate(name, 200), err)
		}
		return "", refusal(CodeCloudflaredMissing, "%s", InstallText())
	}
	return path, nil
}

// hostname resolves a named tunnel's hostname: the one given, else
// ORB_TUNNEL_HOSTNAME, else the saved one. A given one is saved when the
// environment doesn't set it.
func (m *Manager) hostname(env []string, given string) (string, error) {
	fromEnv := envValue(env, HostnameVar)
	raw := given
	switch {
	case raw == "" && fromEnv != "":
		raw = fromEnv
	case raw == "":
		raw = readSettings(m.cfg.Dir).Hostname
	}
	h, err := NormalizeHostname(raw)
	if err != nil {
		return "", err
	}
	if given != "" && fromEnv == "" {
		if err := writeSettings(m.cfg.Dir, settings{Hostname: h}); err != nil {
			return "", fmt.Errorf("save the hostname in %s: %w", SettingsFile, err)
		}
	}
	return h, nil
}

// SaveHostname validates and saves a named tunnel's hostname for later
// starts, and returns it normalized.
func (m *Manager) SaveHostname(raw string) (string, error) {
	h, err := NormalizeHostname(raw)
	if err != nil {
		return "", err
	}
	return h, writeSettings(m.cfg.Dir, settings{Hostname: h})
}

// childEnv is cloudflared's environment: base without any tunnel token or
// URL variables, plus TUNNEL_TOKEN for a named tunnel.
func childEnv(base []string, token string) []string {
	drop := []string{"TUNNEL_TOKEN=", "TUNNEL_TOKEN_FILE=", "TUNNEL_URL=", TokenVar + "=", TokenFileVar + "="}
	out := make([]string, 0, len(base)+1)
next:
	for _, kv := range base {
		for _, d := range drop {
			if strings.HasPrefix(kv, d) {
				continue next
			}
		}
		out = append(out, kv)
	}
	if token != "" {
		out = append(out, "TUNNEL_TOKEN="+token)
	}
	return out
}

// secretsOf lists the strings never to show: the token and the secret
// inside it.
func secretsOf(token string) []string {
	if token == "" {
		return nil
	}
	out := []string{token}
	if s, ok := tokenSecret(token); ok && len(s) >= 8 {
		out = append(out, s)
	}
	return out
}

// redact removes secrets from s.
func redact(s string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, redactedMessage)
		}
	}
	return s
}

// maxLineBytes bounds one line of cloudflared's output.
const maxLineBytes = 8 << 10

// readLines follows cloudflared's output until it ends.
func (m *Manager) readLines(p *process, r io.ReadCloser) {
	defer r.Close()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		text := redact(string(bytes.TrimRight(sc.Bytes(), "\r")), p.secrets)
		if len(text) > maxLineBytes {
			text = truncate(text, maxLineBytes)
		}
		m.line(p, text)
	}
	// A longer line or a read error ends the scan: drain so cloudflared
	// never blocks writing.
	_, _ = io.Copy(io.Discard, r)
}

// line handles one line of output.
func (m *Manager) line(p *process, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	level := levelOf(text)
	m.mu.Lock()
	if m.run != p {
		m.mu.Unlock()
		return
	}
	log := append(m.status.Log, LogLine{Time: time.Now().UTC(), Level: level, Text: text})
	if len(log) > maxLogLines {
		log = log[len(log)-maxLogLines:]
	}
	m.status.Log = log
	if level == "ERR" || level == "FTL" {
		p.lastErr = messageOf(text)
	}
	var announce, connected bool
	if m.status.Mode == ModeQuick && m.status.PublicURL == "" {
		if u, ok := ParseQuickURL(text); ok {
			m.status.PublicURL, m.status.Hostname, announce = u, strings.TrimPrefix(u, "https://"), true
		}
	}
	if connectedLine(text) && m.status.State == StateStarting && m.status.PublicURL != "" {
		now := time.Now().UTC()
		m.status.State, m.status.ConnectedAt, m.status.Problem, connected = StateConnected, &now, "", true
	}
	status := m.snapshot()
	m.publish()
	m.mu.Unlock()

	if announce {
		m.cfg.Logf("orb: tunnel %s → %s (quick: the URL changes on every start)", status.PublicURL, status.Target)
	}
	if connected {
		m.cfg.Logf("orb: tunnel connected: %s is on the internet while it runs (/_dev and the Dev Portal are not)", status.PublicURL)
		go m.autoCheck(p)
	}
	if level == "ERR" || level == "FTL" {
		m.cfg.Logf("orb: cloudflared: %s", messageOf(text))
	}
}

// watchStart fails a start that doesn't connect in time.
func (m *Manager) watchStart(p *process) {
	timer := time.NewTimer(m.cfg.StartTimeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return
	case <-timer.C:
	}
	m.mu.Lock()
	if m.run != p || m.status.State != StateStarting {
		m.mu.Unlock()
		return
	}
	what := "connect to Cloudflare"
	if m.status.Mode == ModeQuick && m.status.PublicURL == "" {
		what = "report a trycloudflare.com URL"
	}
	problem := fmt.Sprintf("cloudflared didn't %s within %s", what, m.cfg.StartTimeout)
	if p.lastErr != "" {
		problem += "; its last error: " + p.lastErr
	}
	m.status.State, m.status.Problem = StateFailed, problem
	p.stopping = true // the failure stays the status after the process ends
	m.publish()
	m.mu.Unlock()
	m.cfg.Logf("orb: tunnel failed: %s", problem)
	m.signalAndWait(p)
}

// exited records the end of a cloudflared process.
func (m *Manager) exited(p *process, err error) {
	m.mu.Lock()
	close(p.done)
	if m.run != p {
		m.mu.Unlock()
		return
	}
	m.run = nil
	failedAlready := m.status.State == StateFailed
	m.status.PID = 0
	switch {
	case failedAlready:
	case p.stopping:
		m.status.State, m.status.Problem, m.status.ConnectedAt = StateOff, "", nil
	default:
		problem := "cloudflared exited"
		if err != nil {
			problem += " (" + redact(err.Error(), p.secrets) + ")"
		}
		if p.lastErr != "" {
			problem += ": " + p.lastErr
		}
		m.status.State, m.status.Problem, m.status.ConnectedAt = StateFailed, problem, nil
	}
	status := m.snapshot()
	m.publish()
	m.mu.Unlock()
	if status.State == StateFailed && !failedAlready {
		m.cfg.Logf("orb: tunnel failed: %s", status.Problem)
	}
}

// Stop stops the tunnel and waits for cloudflared's group to exit.
func (m *Manager) Stop() error {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	return m.stop()
}

func (m *Manager) stop() error {
	m.mu.Lock()
	p := m.run
	if p == nil {
		if m.status.State == StateFailed {
			m.status.State, m.status.Problem = StateOff, ""
			m.publish()
		}
		m.mu.Unlock()
		return nil
	}
	p.stopping = true
	if m.status.State != StateFailed {
		m.status.State = StateStopping
	}
	m.publish()
	m.mu.Unlock()
	m.signalAndWait(p)
	m.mu.Lock()
	if m.status.State == StateFailed && m.run == nil {
		m.status.State, m.status.Problem = StateOff, ""
		m.publish()
	}
	m.mu.Unlock()
	m.cfg.Logf("orb: tunnel stopped")
	return nil
}

// signalAndWait ends p's process group: SIGTERM, then SIGKILL after
// StopTimeout.
func (m *Manager) signalAndWait(p *process) {
	_ = terminate(p.cmd)
	select {
	case <-p.done:
		return
	case <-time.After(m.cfg.StopTimeout):
	}
	_ = kill(p.cmd)
	select {
	case <-p.done:
	case <-time.After(m.cfg.StopTimeout):
		// Output held open by a descendant that ignored SIGKILL's group
		// can't happen on Unix; don't hang orb dev on it elsewhere.
	}
}

// Restart starts the tunnel again with the options of the last start.
func (m *Manager) Restart(ctx context.Context) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.mu.Lock()
	opts, started := m.opts, m.started
	m.mu.Unlock()
	if !started {
		return refusal(CodeNotConnected, "no tunnel was started in this orb dev run")
	}
	return m.start(ctx, opts)
}

// TargetChanged tells the manager the app may listen elsewhere now. A
// running quick tunnel pointing at the old address restarts; a named
// tunnel's target is set in the Cloudflare dashboard, so orb says where it
// must point instead.
func (m *Manager) TargetChanged(ctx context.Context) {
	target, err := m.cfg.Target()
	if err != nil || target == "" {
		return
	}
	m.mu.Lock()
	running := m.run != nil && !m.run.stopping
	old, mode := m.status.Target, m.status.Mode
	if !running || old == target {
		m.mu.Unlock()
		return
	}
	if mode == ModeNamed {
		m.status.Target = target
		m.status.Problem = fmt.Sprintf("the app now listens on %s: point the tunnel's public hostname at it in the Cloudflare dashboard", target)
		m.publish()
		m.mu.Unlock()
		m.cfg.Logf("orb: %s", m.Status().Problem)
		return
	}
	m.mu.Unlock()
	m.cfg.Logf("orb: the app now listens on %s; restarting the tunnel", target)
	go func() { _ = m.Restart(ctx) }()
}

// Close stops the tunnel for good, for a stopping orb dev.
func (m *Manager) Close() {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.mu.Lock()
	m.closed = true
	running := m.run != nil
	m.mu.Unlock()
	if running {
		_ = m.stop()
	}
}

// autoCheck checks reachability after connecting, retrying while DNS and
// Cloudflare's edge catch up.
func (m *Manager) autoCheck(p *process) {
	for attempt := 0; attempt < m.cfg.CheckAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-p.done:
				return
			case <-time.After(m.cfg.CheckInterval):
			}
		}
		m.mu.Lock()
		current := m.run == p && m.status.State == StateConnected
		m.mu.Unlock()
		if !current {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		r, err := m.Check(ctx)
		cancel()
		if err == nil && r.OK {
			m.cfg.Logf("orb: tunnel reachable: %s answered like the app", r.URL)
			return
		}
		if err == nil && attempt == m.cfg.CheckAttempts-1 {
			m.cfg.Logf("orb: tunnel not reachable yet: %s", r.Detail)
		}
	}
}

// Check requests the public URL's /livez and compares the answer with the
// app's own, so a hostname pointing at something else isn't reported as
// working. It refuses when no tunnel is connected.
func (m *Manager) Check(ctx context.Context) (Reachability, error) {
	m.mu.Lock()
	s := m.status
	m.mu.Unlock()
	if s.State != StateConnected || s.PublicURL == "" {
		return Reachability{}, refusal(CodeNotConnected, "no tunnel is connected")
	}
	r := m.check(ctx, s.PublicURL, s.Target)
	m.mu.Lock()
	if m.status.PublicURL == s.PublicURL {
		m.status.Check = &r
		m.publish()
	}
	m.mu.Unlock()
	return r, nil
}

// check does one reachability check.
func (m *Manager) check(ctx context.Context, publicURL, target string) Reachability {
	url := publicURL + "/livez"
	r := Reachability{URL: url}
	start := time.Now()
	status, body, err := m.get(ctx, url)
	r.CheckedAt, r.LatencyMS = time.Now().UTC(), float64(time.Since(start))/float64(time.Millisecond)
	r.Status = status
	switch {
	case err != nil:
		r.Detail = "the request failed: " + err.Error() + " (a new hostname can take a minute to resolve)"
		return r
	case status == 530 || status == 502:
		r.Detail = fmt.Sprintf("Cloudflare answered %d: the tunnel isn't reaching the app at %s", status, target)
		return r
	case status != http.StatusOK:
		r.Detail = fmt.Sprintf("%s answered %d, not 200", url, status)
		return r
	}
	localStatus, localBody, err := m.get(ctx, target+"/livez")
	if err == nil && (localStatus != status || !bytes.Equal(bytes.TrimSpace(localBody), bytes.TrimSpace(body))) {
		r.Detail = fmt.Sprintf("%s answered, but not like the app's /livez at %s: check where the tunnel's public hostname points", url, target)
		return r
	}
	r.OK, r.Detail = true, "reachable: /livez answered 200 through Cloudflare"
	return r
}

func (m *Manager) get(ctx context.Context, url string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", "orb-dev-tunnel-check")
	res, err := m.cfg.Client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, body, err
}

// Info is everything the portal's Tunnel screen shows besides the setup.
type Info struct {
	Status      Status        `json:"status"`
	Cloudflared Binary        `json:"cloudflared"`
	Install     []InstallStep `json:"install"`
	// OS is this machine's operating system, to pick install steps.
	OS string `json:"os"`
	// Hostname is a named tunnel's hostname as configured (normalized when
	// valid) and HostnameSource where it comes from: ORB_TUNNEL_HOSTNAME,
	// the portal's saved setting, or "".
	Hostname       string `json:"hostname,omitempty"`
	HostnameSource string `json:"hostname_source,omitempty"`
	// TokenSource is the variable a named tunnel's token comes from, or ""
	// when none is set; TokenProblem says what is wrong with it. The token
	// itself is never included.
	TokenSource  string `json:"token_source,omitempty"`
	TokenProblem string `json:"token_problem,omitempty"`
	// AppEnv is APP_ENV as orb dev runs the app; Allowed reports a tunnel
	// may start (development only).
	AppEnv  string `json:"app_env"`
	Allowed bool   `json:"allowed"`
	// Target is where a tunnel forwards: the app's address.
	Target string `json:"target,omitempty"`
}

// Info describes cloudflared, the configuration and the status.
func (m *Manager) Info(ctx context.Context, goos string) Info {
	env := m.cfg.Env()
	info := Info{Status: m.Status(), Install: InstallHelp(), OS: goos, AppEnv: envValue(env, "APP_ENV")}
	if info.AppEnv == "" {
		info.AppEnv = "development"
	}
	info.Allowed = info.AppEnv == "development"
	if target, err := m.cfg.Target(); err == nil {
		info.Target = target
	}
	info.Cloudflared = FindBinary(env, m.cfg.LookPath, func(path string) string {
		m.mu.Lock()
		v, ok := m.versions[path]
		m.mu.Unlock()
		if !ok {
			v = Version(ctx, path)
			m.mu.Lock()
			m.versions[path] = v
			m.mu.Unlock()
		}
		return v
	})
	switch {
	case envValue(env, HostnameVar) != "":
		info.HostnameSource, info.Hostname = HostnameVar, envValue(env, HostnameVar)
	case readSettings(m.cfg.Dir).Hostname != "":
		info.HostnameSource, info.Hostname = "settings", readSettings(m.cfg.Dir).Hostname
	}
	if h, err := NormalizeHostname(info.Hostname); err == nil {
		info.Hostname = h
	}
	if _, source, err := LoadToken(env); err == nil {
		info.TokenSource = source
	} else {
		var e *Error
		if errors.As(err, &e) && e.Code != CodeTokenMissing {
			info.TokenProblem = e.Detail
			switch {
			case envValue(env, TokenFileVar) != "":
				info.TokenSource = TokenFileVar
			case envValue(env, TokenVar) != "":
				info.TokenSource = TokenVar
			}
		}
	}
	return info
}
