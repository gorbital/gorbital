// Package devconsole serves development-only JSON APIs under /_dev/ for a
// local console (ADR-0065): what the app wired, its routes, the environment
// variables it read (secrets only as set or unset), recent requests and log
// records with live streams, captured email, migration state and recent job
// runs.
//
// The console exposes an app's internals, so every request must pass three
// checks before anything else runs:
//
//   - the Host header names localhost, 127.0.0.1 or [::1] with the port the
//     request arrived on, which defeats DNS rebinding: a page on another
//     site that rebinds its own name to 127.0.0.1 still sends its own name;
//   - the connection comes from a loopback address, so an app listening on
//     every interface doesn't serve the console to its network;
//   - the request carries no forwarding headers (Forwarded, X-Forwarded-For,
//     X-Forwarded-Host, X-Real-IP, True-Client-IP, CF-Connecting-IP, CF-Ray,
//     CDN-Loop): a reverse proxy or tunnel on this machine, such as
//     cloudflared for orb dev --tunnel, connects from loopback and may even
//     send a local Host, but adds them (ADR-0086);
//   - an Authorization: Bearer header carries the console token (at least
//     [MinTokenLength] characters; orb dev generates 256 bits per run),
//     compared in constant time.
//
// Responses never carry CORS headers, and all say Cache-Control: no-store.
// The app decides whether the console exists at all: gorbital apps mount it
// only when APP_ENV is development and DEV_CONSOLE_TOKEN is set, and refuse
// to start in production with the token set.
//
//	logs, _ := devconsole.NewLogs(devconsole.DefaultMaxLogs)
//	tel, _ := telemetry.Setup(ctx, name, version, telemetry.WithLogTee(logs.Handler()))
//	console, _ := devconsole.New(token, devconsole.WithLogs(logs), devconsole.WithSources(sources))
//	handler = console.Mount(handler, logger)
//
// Stability: stable (ADR-0015, ADR-0065). The Go API follows the stability
// promise; the development-only /_dev responses are described by the OpenAPI
// document served at /_dev/openapi.json ([OpenAPI]) and aren't API: fields may
// be added or change between releases.
package devconsole

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorbital.dev/httpx"
)

const (
	// Prefix is the path every console endpoint starts with.
	Prefix = "/_dev/"
	// MinTokenLength is the shortest console token [New] accepts.
	MinTokenLength = 32
	// MaxTokenLength is the longest console token [New] accepts.
	MaxTokenLength = 512

	// DefaultMaxRequests is how many recent requests the console keeps
	// without [WithMaxRequests].
	DefaultMaxRequests = 500
	// DefaultMaxLogs is how many recent log records [NewLogs] callers
	// usually keep.
	DefaultMaxLogs = 1000
	// DefaultMaxStreams is how many live streams the console serves at once
	// without [WithStreams].
	DefaultMaxStreams = 8
	// DefaultStreamDuration is how long a live stream lasts at most without
	// [WithStreams].
	DefaultStreamDuration = 30 * time.Minute

	// streamBuffer is how many events wait for a slow stream client before
	// newer ones are dropped and counted.
	streamBuffer = 256
	// keepAliveInterval is how often an idle stream sends a comment, so
	// proxies and clients see the connection alive.
	keepAliveInterval = 15 * time.Second
	// writeTimeout bounds each write to a stream, instead of the server's
	// write timeout, which would cut a stream off.
	writeTimeout = 10 * time.Second
	// refusalLogInterval limits log lines about refused requests, so a page
	// sending requests in a loop can't flood the logs.
	refusalLogInterval = time.Second
)

// Errors of the console.
var (
	// ErrInvalidToken reports a console token shorter than
	// [MinTokenLength], longer than [MaxTokenLength], or holding characters
	// other than visible ASCII.
	ErrInvalidToken = fmt.Errorf("devconsole: the token must be %d to %d visible ASCII characters", MinTokenLength, MaxTokenLength)
	// ErrUnavailable wraps errors of sources whose service isn't reachable,
	// such as Mailpit: the endpoint answers 503.
	ErrUnavailable = errors.New("devconsole: unavailable")
	// ErrStreamsClosed is the reason streams end when [Console.Close] is
	// called.
	ErrStreamsClosed = errors.New("devconsole: closed")
)

// CheckToken reports whether token is acceptable as a console token.
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

// Sources provide the console's app-specific sections. A nil source leaves
// its endpoint out: it answers 404, and the index doesn't list it.
type Sources struct {
	// App describes the running app (GET /_dev/app).
	App func(ctx context.Context) (App, error)
	// Routes lists the app's HTTP routes (GET /_dev/routes); see
	// [RoutesFromOpenAPI].
	Routes func(ctx context.Context) ([]Route, error)
	// Config lists the environment variables the app read (GET
	// /_dev/config); see [EnvKeys].
	Config func(ctx context.Context) ([]EnvKey, error)
	// Mail lists captured email (GET /_dev/mail); see [MailpitSource].
	Mail func(ctx context.Context) (Mail, error)
	// Migrations reports the database's migration state (GET
	// /_dev/migrations).
	Migrations func(ctx context.Context) (Migrations, error)
	// Jobs lists recent job runs (GET /_dev/jobs).
	Jobs func(ctx context.Context) ([]JobRun, error)
	// MailPreviews renders the app's emails with sample data (GET
	// /_dev/mail/previews, /_dev/mail/preview, POST /_dev/mail/preview/send);
	// see [MailPreviewer].
	MailPreviews *MailPreviewer
}

// Option configures a [Console].
type Option func(*options)

type options struct {
	port           string
	maxRequests    int
	maxStreams     int
	streamDuration time.Duration
	logs           *Logs
	sources        Sources
}

// WithAddr sets the address the app listens on, whose port the Host header
// must name when the connection's local address isn't known, such as in
// tests calling the handler directly. The local address of a real
// connection always wins.
func WithAddr(addr string) Option {
	return func(o *options) {
		if _, port, err := net.SplitHostPort(addr); err == nil {
			o.port = port
		}
	}
}

// WithMaxRequests keeps the n most recent requests (default
// [DefaultMaxRequests]).
func WithMaxRequests(n int) Option { return func(o *options) { o.maxRequests = n } }

// WithStreams serves at most n live streams at once, each for at most
// duration (defaults [DefaultMaxStreams] and [DefaultStreamDuration]).
func WithStreams(n int, duration time.Duration) Option {
	return func(o *options) { o.maxStreams, o.streamDuration = n, duration }
}

// WithLogs serves logs' records at GET /_dev/logs and its stream.
func WithLogs(logs *Logs) Option { return func(o *options) { o.logs = logs } }

// WithSources sets the app-specific sections.
func WithSources(s Sources) Option { return func(o *options) { o.sources = s } }

// Console serves the development console's APIs. It is safe for concurrent
// use. A nil *Console serves nothing: [Console.Mount] returns the handler
// unchanged, and its other methods do nothing, so apps can hold a nil
// console when it is off.
type Console struct {
	tokenHash      [sha256.Size]byte
	port           string
	logs           *Logs
	sources        Sources
	requests       *buffer[Request]
	maxStreams     int
	streamDuration time.Duration

	mu      sync.Mutex
	streams int
	closed  bool
	close   chan struct{} // closed by Close

	lastRefusalLog atomic.Int64 // unix nanoseconds
}

// New returns a console that accepts token. It returns [ErrInvalidToken]
// for a token [CheckToken] refuses.
func New(token string, opts ...Option) (*Console, error) {
	if err := CheckToken(token); err != nil {
		return nil, err
	}
	o := options{maxRequests: DefaultMaxRequests, maxStreams: DefaultMaxStreams, streamDuration: DefaultStreamDuration}
	for _, opt := range opts {
		opt(&o)
	}
	switch {
	case o.maxRequests < 1:
		return nil, errors.New("devconsole: the maximum number of requests must be positive")
	case o.maxStreams < 1 || o.streamDuration <= 0:
		return nil, errors.New("devconsole: stream limits must be positive")
	}
	return &Console{
		tokenHash:      sha256.Sum256([]byte(token)),
		port:           o.port,
		logs:           o.logs,
		sources:        o.sources,
		requests:       newBuffer[Request](o.maxRequests),
		maxStreams:     o.maxStreams,
		streamDuration: o.streamDuration,
		close:          make(chan struct{}),
	}, nil
}

// Mount returns a handler serving the console for paths under [Prefix]
// (and /_dev itself) and next for every other path. The console runs
// before next's middleware: its requests aren't recorded, logged as access
// or subject to CORS, and they reach no application handler. logger
// receives refused requests and source errors.
func (c *Console) Mount(next http.Handler, logger *slog.Logger) http.Handler {
	if c == nil {
		return next
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	console := c.handler(logger)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_dev" || strings.HasPrefix(r.URL.Path, Prefix) {
			console.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Close ends every live stream and refuses new ones, for a shutting-down
// app: HTTP servers wait for long-lived responses.
func (c *Console) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.close)
	}
}

// endpoint is one console path.
type endpoint struct {
	path    string
	serve   func(w http.ResponseWriter, r *http.Request) error
	present bool
	// post endpoints answer POST instead of GET (the preview send).
	post bool
}

// handler checks every request, then routes it.
func (c *Console) handler(logger *slog.Logger) http.Handler {
	endpoints := c.endpoints()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("X-Frame-Options", "DENY")
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				logger.ErrorContext(r.Context(), "dev console: panic", "path", r.URL.Path, "panic", fmt.Sprint(v))
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusInternalServerError, "internal_error", "the dev console failed; see the app's logs"))
			}
		}()

		if reason := c.refuse(r); reason != "" {
			c.logRefusal(r, logger, reason)
			if reason == refusedForwarded {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusForbidden, "forbidden", "the dev console answers only direct local connections, not requests forwarded by a proxy or tunnel"))
				return
			}
			if reason == refusedToken {
				h.Set("WWW-Authenticate", `Bearer realm="dev console"`)
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUnauthorized, "unauthorized", "send the dev console token printed by orb dev as Authorization: Bearer <token>"))
				return
			}
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusForbidden, "forbidden", "the dev console answers only local connections to http://localhost, 127.0.0.1 or [::1] on the app's port"))
			return
		}

		path := r.URL.Path
		if path == "/_dev" {
			path = Prefix
		}
		e, ok := endpoints[path]
		if !ok || !e.present {
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusNotFound, "not_found", "no dev console endpoint "+path))
			return
		}
		switch {
		case e.post && r.Method != http.MethodPost:
			h.Set("Allow", "POST")
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusMethodNotAllowed, "method_not_allowed", "this dev console endpoint answers POST only"))
			return
		case !e.post && r.Method != http.MethodGet && r.Method != http.MethodHead:
			h.Set("Allow", "GET, HEAD")
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusMethodNotAllowed, "method_not_allowed", "dev console endpoints answer GET only"))
			return
		}
		if err := e.serve(w, r); err != nil {
			if errors.Is(err, ErrUnavailable) {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusServiceUnavailable, "unavailable", err.Error()))
				return
			}
			logger.ErrorContext(r.Context(), "dev console: source failed", "path", path, "error", err)
			httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusInternalServerError, "internal_error", "the dev console couldn't read this section; see the app's logs"))
		}
	})
}

// Reasons a request is refused.
const (
	refusedHost      = "host"
	refusedPeer      = "peer"
	refusedForwarded = "forwarded"
	refusedToken     = "token"
)

// refuse returns why r may not use the console, or "".
func (c *Console) refuse(r *http.Request) string {
	if !allowedHost(r.Host, c.localPort(r)) {
		return refusedHost
	}
	if !loopbackPeer(r.RemoteAddr) {
		return refusedPeer
	}
	if forwarded(r.Header) {
		return refusedForwarded
	}
	if !c.validToken(r.Header.Get("Authorization")) {
		return refusedToken
	}
	return ""
}

// localPort returns the port the request arrived on: the connection's
// local port, else the configured one.
func (c *Console) localPort(r *http.Request) string {
	if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		if _, port, err := net.SplitHostPort(addr.String()); err == nil {
			return port
		}
	}
	return c.port
}

// validToken reports whether header is "Bearer <token>" with the console's
// token. Both sides are hashed first, so the comparison takes the same
// time whatever the length or content of the guess.
func (c *Console) validToken(header string) bool {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	got := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return subtle.ConstantTimeCompare(got[:], c.tokenHash[:]) == 1
}

// logRefusal logs a refused request at most once per refusalLogInterval.
func (c *Console) logRefusal(r *http.Request, logger *slog.Logger, reason string) {
	now := time.Now().UnixNano()
	last := c.lastRefusalLog.Load()
	if now-last < int64(refusalLogInterval) || !c.lastRefusalLog.CompareAndSwap(last, now) {
		return
	}
	logger.WarnContext(r.Context(), "dev console: refused a request", "reason", reason, "remote_addr", r.RemoteAddr, "host", truncate(r.Host, 256))
}

// allowedHost reports whether host, a Host header, names a loopback name
// exactly, with port (or no port when port is 80).
func allowedHost(host, port string) bool {
	if host == "" || port == "" {
		return false
	}
	name, hostPort := host, "80"
	if strings.HasPrefix(host, "[") || strings.Count(host, ":") == 1 {
		n, p, err := net.SplitHostPort(host)
		if err != nil {
			return false
		}
		name, hostPort = n, p
		if strings.HasPrefix(host, "[") {
			name = "[" + n + "]"
		}
	}
	switch strings.ToLower(name) {
	case "localhost", "127.0.0.1", "[::1]":
		return hostPort == port
	}
	return false
}

// forwardingHeaders are set by reverse proxies, CDNs and tunnels to pass on
// the client they received a request from. Cloudflare's tunnel (cloudflared)
// sends CF-Connecting-IP, CF-Ray, CDN-Loop and X-Forwarded-For.
var forwardingHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "True-Client-Ip", "Cf-Connecting-Ip", "Cf-Ray", "Cdn-Loop"}

// forwarded reports whether a request came through a proxy or tunnel: it
// carries any forwarding header, even an empty one.
func forwarded(h http.Header) bool {
	for _, name := range forwardingHeaders {
		if _, ok := h[name]; ok {
			return true
		}
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

// writeJSON writes v as a JSON response. It returns an error only when v
// can't be encoded, before anything is written.
func writeJSON(w http.ResponseWriter, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(append(data, '\n')) // a failed write means the client is gone
	return nil
}

// truncate shortens s to at most n bytes, on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
