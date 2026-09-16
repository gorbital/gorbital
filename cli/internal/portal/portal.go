// Package portal serves the Dev Portal while orb dev runs an app
// (ADR-0066): the portal's web UI, its own API under /_portal/api/ (the
// app's state and output, restarts, generators), and a proxy under
// /_portal/app/ to the app, which adds the dev console token for /_dev/
// requests (ADR-0065) so the UI never holds it.
//
// The portal exposes a developer's project and can change its files, so it
// binds to a loopback address only and every API request must pass, in
// order:
//
//   - a Host header naming localhost, 127.0.0.1 or [::1], which defeats DNS
//     rebinding: a page on another site that rebinds its own name to
//     127.0.0.1 still sends its own name;
//   - a connection from a loopback address;
//   - the portal token, as the orb_portal cookie the /_portal/auth link sets
//     (HttpOnly, SameSite=Strict) or as an Authorization: Bearer header for
//     scripts; compared in constant time;
//   - for anything but GET and HEAD, an X-Orb-Portal header, which a browser
//     sends only after a CORS preflight the portal never answers.
//
// Responses never carry CORS headers, and API responses say Cache-Control:
// no-store. orb dev generates the token per run and prints it once in the
// link it opens; it is never written to disk.
package portal

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"gorbital.dev/cli/internal/genplan"
)

// Paths of the portal.
const (
	// Prefix is where the portal's own endpoints live.
	Prefix = "/_portal/"
	// APIPrefix is the portal's API.
	APIPrefix = Prefix + "api/"
	// AppPrefix proxies to the app: /_portal/app/v1/ping reaches the app's
	// /v1/ping.
	AppPrefix = Prefix + "app/"
	// AuthPath takes the token from the link orb dev prints and sets the
	// cookie.
	AuthPath = Prefix + "auth"

	// CookieName holds the token in the browser.
	CookieName = "orb_portal"
	// MutationHeader must accompany every request that isn't GET or HEAD.
	MutationHeader = "X-Orb-Portal"

	// MinTokenLength is the shortest token New accepts.
	MinTokenLength = 32
	// MaxTokenLength is the longest token New accepts.
	MaxTokenLength = 512

	// DefaultMaxStreams is how many event streams the portal serves at once.
	DefaultMaxStreams = 8
	// DefaultStreamDuration is how long an event stream lasts at most.
	DefaultStreamDuration = 30 * time.Minute

	keepAliveInterval = 15 * time.Second
	writeTimeout      = 10 * time.Second
	refusalLogEvery   = time.Second
)

// ErrInvalidToken reports a token shorter than [MinTokenLength], longer
// than [MaxTokenLength], or holding characters other than visible ASCII.
var ErrInvalidToken = fmt.Errorf("portal: the token must be %d to %d visible ASCII characters", MinTokenLength, MaxTokenLength)

// CheckToken reports whether token is acceptable as a portal token.
func CheckToken(token string) error {
	if len(token) < MinTokenLength || len(token) > MaxTokenLength {
		return ErrInvalidToken
	}
	for i := range len(token) {
		if token[i] <= ' ' || token[i] > '~' {
			return ErrInvalidToken
		}
	}
	return nil
}

// Project describes the app orb dev runs, from gorbital.yaml.
type Project struct {
	Name     string   `json:"name"`
	Module   string   `json:"module"`
	Preset   string   `json:"preset"`
	Tenancy  string   `json:"tenancy,omitempty"`
	Features []string `json:"features"`
	// Mail is the email provider recorded in the manifest, if any.
	Mail string `json:"mail,omitempty"`
	// Dir is the app directory on this machine.
	Dir string `json:"dir"`
	// Database reports whether the app has PostgreSQL (the Full preset).
	Database bool `json:"database"`
}

// Generator plans and applies one generator (job, resource, migration) from
// the JSON input the UI sends, which has the same fields as the CLI's flags.
type Generator struct {
	// Plan returns what the generator would write, without writing.
	Plan func(ctx context.Context, input json.RawMessage) (genplan.Plan, error)
	// Apply plans again and writes; allowDirty skips the clean-git check.
	Apply func(ctx context.Context, input json.RawMessage, allowDirty bool) (genplan.Plan, error)
}

// Config configures a [Server].
type Config struct {
	// Token is the portal token; see [CheckToken].
	Token string
	// Version is the orb version, reported in the status.
	Version string
	Project Project
	// Supervisor controls the app; Hub carries its output and state.
	Supervisor Supervisor
	Hub        *Hub
	// ConsoleToken is the app's dev console token, added to proxied /_dev/
	// requests; empty when the app has no console.
	ConsoleToken string
	// Links are addresses worth showing: api, docs, mail, console,
	// grafana; orb dev fills in those that apply.
	Links map[string]string
	// Generators by name: job, resource, migration.
	Generators map[string]Generator
	// Jobs lists the app's jobs as they are in code (ADR-0071); nil when
	// the app has none.
	Jobs func() ([]JobSource, error)
	// Logs is the local log store (ADR-0072); nil serves no log endpoints.
	Logs *LogStore
	// System samples the machine and the app process (ADR-0073); nil
	// answers 404.
	System *SystemSampler
	// Health checks every service the app depends on; nil reports none.
	Health func(ctx context.Context) []ServiceHealth
	// Mail is the mail catcher's inbox (ADR-0074).
	Mail MailConfig
	// Env edits the app's .env (ADR-0074); nil answers 404.
	Env *EnvEditor
	// Database connects the Table Editor and Schema pages to the app's
	// database; an empty Open means the app has none.
	Database DatabaseConfig
	// SQL keeps the SQL editor's snippets and history; nil in an app
	// without a database.
	SQL *SQLStore
	// UI is the built portal UI (a Next.js static export) to serve at /.
	// Nil, or a UI without index.html, serves a placeholder page saying how
	// to get it.
	UI fs.FS
	// Logf receives refused requests and errors; nil discards them.
	Logf func(format string, args ...any)
	// MaxStreams and StreamDuration bound event streams; zero means the
	// defaults.
	MaxStreams     int
	StreamDuration time.Duration
}

// Server serves the portal. It is safe for concurrent use.
type Server struct {
	cfg       Config
	tokenHash [sha256.Size]byte
	static    http.Handler
	bundled   bool
	proxy     http.Handler
	mux       *http.ServeMux
	startedAt time.Time

	streams        atomic.Int32
	lastRefusalLog atomic.Int64
	closed         chan struct{}
	closeOnce      atomic.Bool
}

// New returns a server for cfg. It returns [ErrInvalidToken] for a token
// [CheckToken] refuses, and an error when the supervisor or hub is missing.
func New(cfg Config) (*Server, error) {
	if err := CheckToken(cfg.Token); err != nil {
		return nil, err
	}
	if cfg.Supervisor == nil || cfg.Hub == nil {
		return nil, errors.New("portal: a supervisor and a hub are required")
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	if cfg.MaxStreams <= 0 {
		cfg.MaxStreams = DefaultMaxStreams
	}
	if cfg.StreamDuration <= 0 {
		cfg.StreamDuration = DefaultStreamDuration
	}
	if cfg.Links == nil {
		cfg.Links = map[string]string{}
	}
	s := &Server{cfg: cfg, tokenHash: sha256.Sum256([]byte(cfg.Token)), startedAt: time.Now(), closed: make(chan struct{})}
	s.static, s.bundled = newStatic(cfg.UI)
	s.proxy = s.newProxy()
	s.mux = s.routes()
	return s, nil
}

// Handler returns the portal's HTTP handler.
func (s *Server) Handler() http.Handler { return s.mux }

// Close ends every event stream, for a shutting-down orb dev.
func (s *Server) Close() {
	if s.closeOnce.CompareAndSwap(false, true) {
		close(s.closed)
	}
}

// routes builds the mux: the auth link and the UI are open to anyone on
// this machine (the UI holds nothing secret); the API and the proxy need
// the token.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+AuthPath, s.serveAuth)
	mux.Handle(APIPrefix+"db/sql/", s.guard(s.sqlHandler()))
	mux.Handle(APIPrefix+"db/", s.guard(s.dbHandler()))
	mux.Handle(APIPrefix, s.guard(s.apiHandler()))
	mux.Handle(AppPrefix, s.guard(http.StripPrefix(strings.TrimSuffix(AppPrefix, "/"), s.proxy)))
	mux.Handle(Prefix, s.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "not_found", "no portal endpoint "+r.URL.Path)
	})))
	mux.Handle("/", s.static)
	return mux
}

// Reasons a request is refused.
const (
	refusedHost     = "host"
	refusedPeer     = "peer"
	refusedToken    = "token"
	refusedMutation = "mutation_header"
)

// guard runs the checks in the package documentation before next.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		switch reason := s.refuse(r); reason {
		case "":
			next.ServeHTTP(w, r)
		case refusedToken:
			s.logRefusal(r, reason)
			h.Set("WWW-Authenticate", `Bearer realm="dev portal"`)
			writeProblem(w, http.StatusUnauthorized, "unauthorized", "open the Dev Portal link orb dev printed, or send its token as Authorization: Bearer <token>")
		case refusedMutation:
			s.logRefusal(r, reason)
			writeProblem(w, http.StatusForbidden, "forbidden", "requests that change something must carry the "+MutationHeader+" header")
		default:
			s.logRefusal(r, reason)
			writeProblem(w, http.StatusForbidden, "forbidden", "the Dev Portal answers only local connections to http://localhost or http://127.0.0.1")
		}
	})
}

// refuse returns why r may not use the portal, or "".
func (s *Server) refuse(r *http.Request) string {
	if !loopbackHost(r.Host) {
		return refusedHost
	}
	if !loopbackPeer(r.RemoteAddr) {
		return refusedPeer
	}
	if !s.authenticated(r) {
		return refusedToken
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(MutationHeader) == "" {
		return refusedMutation
	}
	return ""
}

// authenticated reports whether r carries the token, as the cookie or as a
// bearer header.
func (s *Server) authenticated(r *http.Request) bool {
	if c, err := r.Cookie(CookieName); err == nil && s.tokenMatches(c.Value) {
		return true
	}
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	return ok && strings.EqualFold(scheme, "Bearer") && s.tokenMatches(strings.TrimSpace(token))
}

// tokenMatches compares in constant time whatever the guess's length.
func (s *Server) tokenMatches(guess string) bool {
	got := sha256.Sum256([]byte(guess))
	return subtle.ConstantTimeCompare(got[:], s.tokenHash[:]) == 1
}

// serveAuth takes the token from the printed link, sets the cookie and
// sends the browser to the UI. A wrong token gets an explanation, never a
// cookie.
func (s *Server) serveAuth(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	switch {
	case !loopbackHost(r.Host) || !loopbackPeer(r.RemoteAddr):
		s.logRefusal(r, refusedHost)
		http.Error(w, "the Dev Portal answers only local connections to http://localhost or http://127.0.0.1", http.StatusForbidden)
		return
	case !s.tokenMatches(r.URL.Query().Get("t")):
		s.logRefusal(r, refusedToken)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, authFailedPage)
		return
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // no Secure: the portal is plain http on a loopback address only
		Name: CookieName, Value: r.URL.Query().Get("t"), Path: "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logRefusal logs a refused request at most once per refusalLogEvery.
func (s *Server) logRefusal(r *http.Request, reason string) {
	now := time.Now().UnixNano()
	last := s.lastRefusalLog.Load()
	if now-last < int64(refusalLogEvery) || !s.lastRefusalLog.CompareAndSwap(last, now) {
		return
	}
	s.cfg.Logf("orb: dev portal refused a request (%s) from %s with Host %q", reason, r.RemoteAddr, truncate(r.Host, 256))
}

// loopbackHost reports whether host, a Host header, names localhost,
// 127.0.0.1 or [::1], with any port.
func loopbackHost(host string) bool {
	if host == "" {
		return false
	}
	name := host
	if strings.HasPrefix(host, "[") || strings.Count(host, ":") == 1 {
		n, _, err := net.SplitHostPort(host)
		if err != nil {
			// An IPv6 literal without a port, such as [::1].
			if !strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]") {
				return false
			}
			n = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		}
		name = n
	}
	switch strings.ToLower(name) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// loopbackPeer reports whether remoteAddr, a connection's peer address, is
// a loopback address.
func loopbackPeer(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// problem is an error response, shaped like the app's problem+json
// (httpx.Problem) so the UI handles both the same way.
type problem struct {
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

func writeProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	data, _ := json.Marshal(problem{Title: http.StatusText(status), Status: status, Code: code, Detail: detail})
	_, _ = w.Write(append(data, '\n'))
}

// writeJSON writes v as a JSON response. It returns an error only when v
// can't be encoded, before anything is written.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
	return nil
}

// truncate shortens s to at most n bytes, on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

// authFailedPage explains a link from another orb dev run.
var authFailedPage = pageHTML("Dev Portal: link expired", `
<h1>This link is from another <code>orb dev</code> run</h1>
<p>Every <code>orb dev</code> run prints a new Dev Portal link. Use the one in the terminal that is running now.</p>
`)

// pageHTML wraps body in the portal's plain page: dark, the brand colours,
// no scripts.
func pageHTML(title, body string) string {
	return `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>` + html.EscapeString(title) + `</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0B0C0A;color:#F2F1EC;font:14px/1.6 ui-sans-serif,system-ui,sans-serif}
main{max-width:560px;padding:32px;border:1px solid #2B2F23;border-radius:14px;background:#16180F}
h1{font-size:18px;margin:0 0 12px}code,pre{font-family:ui-monospace,monospace;color:#C6F24A}
pre{background:#090A08;padding:12px 14px;border-radius:8px;overflow:auto}p{color:#A8AB9F;margin:8px 0}a{color:#C6F24A}
</style></head><body><main>` + body + `</main></body></html>`
}
