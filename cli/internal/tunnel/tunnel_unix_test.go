//go:build unix

package tunnel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as a fake cloudflared: with FAKE_CLOUDFLARED set
// it behaves as the value says instead of running tests.
func TestMain(m *testing.M) {
	if behaviour := os.Getenv("FAKE_CLOUDFLARED"); behaviour != "" {
		fakeCloudflared(behaviour)
		return
	}
	os.Exit(m.Run())
}

const (
	fakeSecret = "c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0"
	fakeQuick  = "https://calm-river-demo-tunnel.trycloudflare.com"
)

var fakeToken = base64.StdEncoding.EncodeToString([]byte(`{"a":"account-tag","t":"6ff42ae2-765d-4adf-8112-31c55c1551ef","s":"` + fakeSecret + `"}`))

func fakeCloudflared(behaviour string) {
	logf := func(level, format string, args ...any) {
		fmt.Fprintf(os.Stderr, "%s %s %s\n", time.Now().UTC().Format(time.RFC3339), level, fmt.Sprintf(format, args...))
	}
	if dir := os.Getenv("FAKE_DIR"); dir != "" && behaviour != "child" {
		_ = os.WriteFile(filepath.Join(dir, "args"), []byte(strings.Join(os.Args[1:], "\n")), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "token"), []byte(os.Getenv("TUNNEL_TOKEN")), 0o600)
		// A grandchild in the same process group, which must not outlive
		// orb dev either.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "FAKE_CLOUDFLARED=child", "FAKE_DIR=")
		if child.Start() == nil {
			_ = os.WriteFile(filepath.Join(dir, "pids"), []byte(fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid)), 0o600)
		}
	}
	switch behaviour {
	case "child":
		time.Sleep(time.Hour)
		return
	case "quick":
		logf("INF", "Requesting new quick Tunnel on trycloudflare.com...")
		logf("INF", "+--------------------------------------------------------------------------------------------+")
		logf("INF", "|  Your quick Tunnel has been created! Visit it at (it may take some time to be reachable):  |")
		logf("INF", "|  %s                                           |", fakeQuick)
		logf("INF", "+--------------------------------------------------------------------------------------------+")
		logf("INF", "Registered tunnel connection connIndex=0 connection=0a1b event=0 ip=198.41.200.13 location=ams01 protocol=quic")
	case "named":
		token := os.Getenv("TUNNEL_TOKEN")
		logf("INF", "Starting tunnel tunnelID=6ff42ae2 token=%s", token)
		logf("ERR", "pretend failure printing the secret %s", fakeSecret)
		logf("INF", "Registered tunnel connection connIndex=0 connection=0a1b event=0 protocol=quic")
	case "crash":
		logf("ERR", "Couldn't start tunnel error=\"quick tunnels are not supported with a config.yml\"")
		os.Exit(1)
	case "silent":
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		logf("INF", "ignoring SIGTERM")
	}
	time.Sleep(time.Hour)
}

// harness is a manager running the fake.
type harness struct {
	t      *testing.T
	m      *Manager
	dir    string
	env    []string
	target string

	mu       sync.Mutex
	statuses []Status
	logs     []string
}

func newHarness(t *testing.T, behaviour string, env ...string) *harness {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, dir: t.TempDir(), env: append([]string{"APP_ENV=development"}, env...), target: "http://127.0.0.1:18090"}
	h.m = New(Config{
		Dir:        h.dir,
		Env:        func() []string { h.mu.Lock(); defer h.mu.Unlock(); return append([]string{}, h.env...) },
		Target:     func() (string, error) { h.mu.Lock(); defer h.mu.Unlock(); return h.target, nil },
		LookPath:   func(string) (string, error) { return exe, nil },
		ProcessEnv: func() []string { return append(os.Environ(), "FAKE_CLOUDFLARED="+behaviour, "FAKE_DIR="+h.dir) },
		OnChange: func(s Status) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.statuses = append(h.statuses, s)
		},
		Logf: func(format string, args ...any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.logs = append(h.logs, fmt.Sprintf(format, args...))
		},
		StartTimeout:  10 * time.Second,
		StopTimeout:   2 * time.Second,
		CheckAttempts: -1, // no automatic check: tests use no network
		CheckInterval: 10 * time.Millisecond,
		Client:        &http.Client{Timeout: 500 * time.Millisecond},
	})
	t.Cleanup(h.m.Close)
	return h
}

// waitFor polls the status until ok or fails.
func (h *harness) waitFor(what string, ok func(Status) bool) Status {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := h.m.Status(); ok(s) {
			return s
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s; status %+v", what, h.m.Status())
	return Status{}
}

// pids returns the fake's PID and its grandchild's.
func (h *harness) pids() []int {
	h.t.Helper()
	var data []byte
	for range 500 {
		var err error
		if data, err = os.ReadFile(filepath.Join(h.dir, "pids")); err == nil && len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var out []int
	for _, f := range strings.Fields(string(data)) {
		n, _ := strconv.Atoi(f)
		out = append(out, n)
	}
	if len(out) != 2 {
		h.t.Fatalf("pids file %q", data)
	}
	return out
}

// assertGone fails unless every pid has exited.
func assertGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for {
			err := syscall.Kill(pid, 0)
			if errors.Is(err, syscall.ESRCH) {
				break
			}
			// A zombie whose parent died is reaped by init shortly.
			if time.Now().After(deadline) {
				t.Errorf("process %d is still running (%v)", pid, err)
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestQuickTunnelLifecycle(t *testing.T) {
	h := newHarness(t, "quick")
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	s := h.waitFor("connected", func(s Status) bool { return s.State == StateConnected })
	if s.PublicURL != fakeQuick || s.Hostname != strings.TrimPrefix(fakeQuick, "https://") || s.Stable || s.Mode != ModeQuick || s.Target != h.target || s.PID == 0 || s.ConnectedAt == nil {
		t.Errorf("status %+v", s)
	}
	args, _ := os.ReadFile(filepath.Join(h.dir, "args"))
	if got := strings.ReplaceAll(string(args), "\n", " "); got != "tunnel --url http://127.0.0.1:18090 --no-autoupdate" {
		t.Errorf("cloudflared arguments %q", got)
	}
	if token, _ := os.ReadFile(filepath.Join(h.dir, "token")); len(token) != 0 {
		t.Errorf("a quick tunnel got TUNNEL_TOKEN %q", token)
	}
	pids := h.pids()
	if len(s.Log) < 5 {
		t.Errorf("log %v", s.Log)
	}

	if err := h.m.Stop(); err != nil {
		t.Fatal(err)
	}
	if s := h.m.Status(); s.State != StateOff || s.PID != 0 || s.Problem != "" {
		t.Errorf("after Stop: %+v", s)
	}
	assertGone(t, pids)

	// OnChange saw starting, connected, stopping and off.
	seen := map[State]bool{}
	h.mu.Lock()
	for _, s := range h.statuses {
		seen[s.State] = true
	}
	logs := strings.Join(h.logs, "\n")
	h.mu.Unlock()
	for _, st := range []State{StateStarting, StateConnected, StateStopping, StateOff} {
		if !seen[st] {
			t.Errorf("OnChange never saw %s", st)
		}
	}
	for _, want := range []string{"orb: starting a quick tunnel to http://127.0.0.1:18090", "orb: tunnel " + fakeQuick, "orb: tunnel stopped"} {
		if !strings.Contains(logs, want) {
			t.Errorf("messages lack %q:\n%s", want, logs)
		}
	}
}

func TestNamedTunnelNeverLeaksTheToken(t *testing.T) {
	h := newHarness(t, "named", TokenVar+"="+fakeToken, HostnameVar+"=Dev-API.Example.com")
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeNamed}); err != nil {
		t.Fatal(err)
	}
	s := h.waitFor("connected", func(s Status) bool { return s.State == StateConnected })
	if s.PublicURL != "https://dev-api.example.com" || !s.Stable || s.Hostname != "dev-api.example.com" {
		t.Errorf("status %+v", s)
	}
	// cloudflared gets the token in its environment, never its arguments.
	args, _ := os.ReadFile(filepath.Join(h.dir, "args"))
	if got := strings.ReplaceAll(string(args), "\n", " "); got != "tunnel --no-autoupdate run" {
		t.Errorf("cloudflared arguments %q", got)
	}
	if token, _ := os.ReadFile(filepath.Join(h.dir, "token")); string(token) != fakeToken {
		t.Errorf("TUNNEL_TOKEN = %q", token)
	}
	pids := h.pids()

	info := h.m.Info(context.Background(), "darwin")
	if info.TokenSource != TokenVar || info.TokenProblem != "" || info.Hostname != "dev-api.example.com" || info.HostnameSource != HostnameVar {
		t.Errorf("info %+v", info)
	}
	h.m.Close()
	assertGone(t, pids)

	h.mu.Lock()
	everything, _ := json.Marshal(struct {
		S []Status
		L []string
		I Info
	}{h.statuses, h.logs, info})
	h.mu.Unlock()
	for _, secret := range []string{fakeToken, fakeSecret} {
		if strings.Contains(string(everything), secret) {
			t.Errorf("a status, message or info holds a secret: %s", everything)
		}
	}
	if !strings.Contains(string(everything), "token=[redacted]") || !strings.Contains(string(everything), "the secret [redacted]") {
		t.Errorf("the lines weren't kept with the secrets redacted: %s", everything)
	}
	// Nothing was written besides the fake's own files.
	if _, err := os.Stat(filepath.Join(h.dir, filepath.FromSlash(SettingsFile))); err == nil {
		t.Error("a hostname from the environment was saved")
	}
}

func TestNamedTunnelSavesAHostnameFromThePortal(t *testing.T) {
	h := newHarness(t, "named", TokenVar+"="+fakeToken)
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeNamed}); err == nil || errCode(err) != CodeHostnameMissing {
		t.Fatalf("without a hostname: %v", err)
	}
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeNamed, Hostname: "https://tunnel.example.org/"}); err != nil {
		t.Fatal(err)
	}
	h.waitFor("connected", func(s Status) bool { return s.State == StateConnected })
	data, err := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(SettingsFile)))
	if err != nil || strings.Contains(string(data), fakeToken) || !strings.Contains(string(data), `"tunnel.example.org"`) {
		t.Errorf("settings %q, %v", data, err)
	}
	if info := h.m.Info(context.Background(), "linux"); info.Hostname != "tunnel.example.org" || info.HostnameSource != "settings" {
		t.Errorf("info %+v", info)
	}
	// A restart uses the saved hostname.
	if err := h.m.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := h.waitFor("reconnected", func(s Status) bool { return s.State == StateConnected && s.Restarts == 1 })
	if s.Hostname != "tunnel.example.org" {
		t.Errorf("after restart %+v", s)
	}
}

func errCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestStartRefusals(t *testing.T) {
	exe, _ := os.Executable()
	base := func(env ...string) Config {
		return Config{
			Dir:      t.TempDir(),
			Env:      func() []string { return env },
			Target:   func() (string, error) { return "http://127.0.0.1:18090", nil },
			LookPath: func(string) (string, error) { return exe, nil },
		}
	}
	missing := base("APP_ENV=development")
	missing.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	override := base("APP_ENV=development", BinaryVar+"=/opt/nowhere/cloudflared")
	override.LookPath = missing.LookPath
	noTarget := base()
	noTarget.Target = func() (string, error) { return "", errors.New("APP_ADDR unset") }
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("not-a-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		cfg    Config
		opts   StartOptions
		code   string
		detail string
	}{
		{"unknown mode", base(), StartOptions{Mode: "public"}, CodeInvalidMode, "use quick or named"},
		{"missing cloudflared", missing, StartOptions{Mode: ModeQuick}, CodeCloudflaredMissing, "never downloads it"},
		{"ORB_CLOUDFLARED missing", override, StartOptions{Mode: ModeQuick}, CodeCloudflaredMissing, BinaryVar + "=/opt/nowhere/cloudflared"},
		{"production", base("APP_ENV=production"), StartOptions{Mode: ModeQuick}, CodeNotDevelopment, "only for development"},
		{"no app address", noTarget, StartOptions{Mode: ModeQuick}, CodeNoTarget, "APP_ADDR"},
		{"named without token", base(), StartOptions{Mode: ModeNamed, Hostname: "dev.example.com"}, CodeTokenMissing, TokenVar},
		{"named with a bad token", base(TokenVar + "=secret-looking-value-123"), StartOptions{Mode: ModeNamed, Hostname: "dev.example.com"}, CodeTokenInvalid, "isn't a Cloudflare tunnel token"},
		{"named with a bad token file", base(TokenFileVar + "=" + tokenFile), StartOptions{Mode: ModeNamed, Hostname: "dev.example.com"}, CodeTokenInvalid, TokenFileVar},
		{"named with both token forms", base(TokenVar+"="+fakeToken, TokenFileVar+"="+tokenFile), StartOptions{Mode: ModeNamed, Hostname: "dev.example.com"}, CodeTokenInvalid, "keep one"},
		{"named with a quick hostname", base(TokenVar + "=" + fakeToken), StartOptions{Mode: ModeNamed, Hostname: "x.trycloudflare.com"}, CodeHostnameInvalid, "quick tunnel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(tt.cfg)
			err := m.Start(context.Background(), tt.opts)
			if errCode(err) != tt.code || !strings.Contains(err.Error(), tt.detail) {
				t.Errorf("Start = %v (code %q), want %s mentioning %q", err, errCode(err), tt.code, tt.detail)
			}
			if err != nil && (strings.Contains(err.Error(), fakeToken) || strings.Contains(err.Error(), "secret-looking-value-123")) {
				t.Errorf("the error holds the token: %v", err)
			}
			if s := m.Status(); s.State != StateOff {
				t.Errorf("status after a refusal: %+v", s)
			}
		})
	}
}

func TestStartTimeoutStopsCloudflared(t *testing.T) {
	h := newHarness(t, "silent")
	h.m.cfg.StartTimeout = 300 * time.Millisecond
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	pids := h.pids()
	s := h.waitFor("failed", func(s Status) bool { return s.State == StateFailed && s.PID == 0 })
	if !strings.Contains(s.Problem, "didn't report a trycloudflare.com URL within 300ms") {
		t.Errorf("problem %q", s.Problem)
	}
	assertGone(t, pids)
	if err := h.m.Stop(); err != nil || h.m.Status().State != StateOff {
		t.Errorf("Stop after a failure: %v, %+v", err, h.m.Status())
	}
}

func TestCloudflaredExitIsAFailure(t *testing.T) {
	h := newHarness(t, "crash")
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	s := h.waitFor("failed", func(s Status) bool { return s.State == StateFailed })
	if !strings.Contains(s.Problem, "exit status 1") || !strings.Contains(s.Problem, "not supported with a config.yml") {
		t.Errorf("problem %q", s.Problem)
	}
	assertGone(t, h.pids()) // the group goes with cloudflared, even when it exits by itself
}

func TestStopKillsAGroupIgnoringSIGTERM(t *testing.T) {
	h := newHarness(t, "ignore-term")
	h.m.cfg.StopTimeout = 200 * time.Millisecond
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	pids := h.pids()
	h.waitFor("ignoring", func(s Status) bool { return len(s.Log) > 0 })
	start := time.Now()
	if err := h.m.Stop(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("Stop took %s", time.Since(start))
	}
	assertGone(t, pids)
}

func TestCloseRefusesNewStarts(t *testing.T) {
	h := newHarness(t, "quick")
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	pids := h.pids()
	h.m.Close()
	assertGone(t, pids)
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err == nil {
		t.Error("Start after Close succeeded")
	}
}

func TestTargetChangeRestartsAQuickTunnel(t *testing.T) {
	h := newHarness(t, "quick")
	if err := h.m.Start(context.Background(), StartOptions{Mode: ModeQuick}); err != nil {
		t.Fatal(err)
	}
	h.waitFor("connected", func(s Status) bool { return s.State == StateConnected })
	h.m.TargetChanged(context.Background()) // unchanged: nothing
	if s := h.m.Status(); s.Restarts != 0 {
		t.Fatalf("restarted without a change: %+v", s)
	}
	first := h.pids()
	h.mu.Lock()
	h.target = "http://127.0.0.1:18091"
	h.mu.Unlock()
	h.m.TargetChanged(context.Background())
	s := h.waitFor("restarted", func(s Status) bool {
		return s.State == StateConnected && s.Restarts == 1 && s.Target == "http://127.0.0.1:18091"
	})
	_ = s
	assertGone(t, first)
	args, _ := os.ReadFile(filepath.Join(h.dir, "args"))
	if !strings.Contains(string(args), "http://127.0.0.1:18091") {
		t.Errorf("restarted with %q", args)
	}
}

func TestReachabilityCheck(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer app.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `portal`)
	}))
	defer other.Close()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(530)
	}))
	defer down.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, app.URL+"/livez", http.StatusFound)
	}))
	defer redirect.Close()

	m := New(Config{})
	ctx := context.Background()
	if r := m.check(ctx, app.URL, app.URL); !r.OK || r.Status != 200 || r.URL != app.URL+"/livez" {
		t.Errorf("same app: %+v", r)
	}
	if r := m.check(ctx, other.URL, app.URL); r.OK || !strings.Contains(r.Detail, "not like the app's /livez") {
		t.Errorf("another server: %+v", r)
	}
	if r := m.check(ctx, down.URL, app.URL); r.OK || !strings.Contains(r.Detail, "Cloudflare answered 530") {
		t.Errorf("530: %+v", r)
	}
	if r := m.check(ctx, redirect.URL, app.URL); r.OK || r.Status != http.StatusFound {
		t.Errorf("redirect followed: %+v", r)
	}
	if r := m.check(ctx, "http://127.0.0.1:1", app.URL); r.OK || !strings.Contains(r.Detail, "request failed") {
		t.Errorf("unreachable: %+v", r)
	}
	if _, err := m.Check(ctx); errCode(err) != CodeNotConnected {
		t.Errorf("Check without a tunnel: %v", err)
	}
}
