# httpx

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/httpx"
```

Package httpx provides the HTTP foundation of a gorbital app: a server that runs under app.Run with safe timeouts, security middleware, and the RFC 9457 problem+json error contract with an application-owned error mapping (ADR-0018).

A typical middleware chain, outermost first:

```go
handler := httpx.Chain(mux,
	httpx.Recover(logger),
	httpx.RequestID(),
	httpx.AccessLog(logger),
	httpx.SecureHeaders(httpx.SecureHeadersOptions{}),
	cors,
	crossOrigin,
	httpx.BodyLimit(1<<20),
)
```

Stability: stable (ADR-0015, ADR-0054).

## When to use

Every app uses httpx: generated apps build the server and the middleware chain in `internal/app/routes.go`, and map errors to problem+json responses with one [Mapper](#Mapper). Reach for it directly when you add a middleware, map a new error to a status and code, or write a problem response from a handler.

- [Life of a request](../guides/request-lifecycle.md): the middleware chain in order, and what each step does.
- [Error handling](../guides/error-handling.md): errors, mappings and problem codes.
- [Security layers](../guides/security-layers.md): request timeouts with [httpx/timeout](httpx-timeout.md) and network restrictions with [httpx/ipfilter](httpx-ipfilter.md), packages of their own so the error codes they add don't change the recorded surface of apps that don't use them.

## Contents

- Constants: [`DefaultReadHeaderTimeout`](#DefaultReadHeaderTimeout), [`DefaultReadTimeout`](#DefaultReadTimeout), [`DefaultWriteTimeout`](#DefaultWriteTimeout), [`DefaultIdleTimeout`](#DefaultIdleTimeout), [`DefaultShutdownTimeout`](#DefaultShutdownTimeout), [`DefaultMaxHeaderBytes`](#DefaultMaxHeaderBytes), [`DefaultMaintenanceMessage`](#DefaultMaintenanceMessage), [`ProblemContentType`](#ProblemContentType)
- Variables: [`ErrTrustAll`](#ErrTrustAll)
- Functions: [`Chain`](#Chain), [`DefaultCode`](#DefaultCode), [`ParseTrustedProxies`](#ParseTrustedProxies), [`WriteProblem`](#WriteProblem)
- Types:
  - [`AccessNote`](#AccessNote): [`AccessNoteFrom`](#AccessNoteFrom), [`AccessNote.Add`](#AccessNote.Add)
  - [`CORSOptions`](#CORSOptions)
  - [`Captured`](#Captured): [`Capture`](#Capture), [`Captured.Bytes`](#Captured.Bytes), [`Captured.Status`](#Captured.Status), [`Captured.Unwrap`](#Captured.Unwrap), [`Captured.Write`](#Captured.Write), [`Captured.WriteHeader`](#Captured.WriteHeader), [`Captured.WroteHeader`](#Captured.WroteHeader)
  - [`FieldError`](#FieldError)
  - [`MaintenanceOptions`](#MaintenanceOptions)
  - [`Mapper`](#Mapper): [`NewMapper`](#NewMapper), [`Mapper.Add`](#Mapper.Add), [`Mapper.Match`](#Mapper.Match), [`Mapper.Problem`](#Mapper.Problem), [`Mapper.Write`](#Mapper.Write)
  - [`Mapping`](#Mapping)
  - [`Middleware`](#Middleware): [`AccessLog`](#AccessLog), [`BodyLimit`](#BodyLimit), [`CORS`](#CORS), [`CrossOrigin`](#CrossOrigin), [`Maintenance`](#Maintenance), [`Recover`](#Recover), [`RequestID`](#RequestID), [`RequestIDFrom`](#RequestIDFrom), [`SecureHeaders`](#SecureHeaders), [`TrustedProxies`](#TrustedProxies)
  - [`Problem`](#Problem): [`NewProblem`](#NewProblem), [`Problem.ContentType`](#Problem.ContentType), [`Problem.Error`](#Problem.Error), [`Problem.GetStatus`](#Problem.GetStatus)
  - [`SecureHeadersOptions`](#SecureHeadersOptions)
  - [`Server`](#Server): [`NewServer`](#NewServer), [`Server.Addr`](#Server.Addr), [`Server.Run`](#Server.Run)
  - [`ServerOption`](#ServerOption): [`WithErrorLogger`](#WithErrorLogger), [`WithShutdownTimeout`](#WithShutdownTimeout), [`WithTimeouts`](#WithTimeouts)

## Constants

<a id="DefaultReadHeaderTimeout"></a>
<a id="DefaultReadTimeout"></a>
<a id="DefaultWriteTimeout"></a>
<a id="DefaultIdleTimeout"></a>
<a id="DefaultShutdownTimeout"></a>
<a id="DefaultMaxHeaderBytes"></a>

```go
const (
	DefaultReadHeaderTimeout = 5 * time.Second
	DefaultReadTimeout       = 30 * time.Second
	DefaultWriteTimeout      = 60 * time.Second
	DefaultIdleTimeout       = 120 * time.Second
	DefaultShutdownTimeout   = 20 * time.Second
	DefaultMaxHeaderBytes    = 1 << 20
)
```

Server timeouts used by [NewServer](#NewServer) unless overridden.

*Since `v0.1.0`*

<a id="DefaultMaintenanceMessage"></a>

```go
const DefaultMaintenanceMessage = "the service is down for maintenance; try again later"
```

DefaultMaintenanceMessage is the problem detail [Maintenance](#Maintenance) sends while MaintenanceOptions.Message is unset or empty.

*Since `v0.2.0 (unreleased)`*

<a id="ProblemContentType"></a>

```go
const ProblemContentType = "application/problem+json"
```

ProblemContentType is the media type of problem responses.

*Since `v0.1.0`*

## Variables

<a id="ErrTrustAll"></a>

```go
var ErrTrustAll = errors.New("httpx: a trusted proxy range can't cover every address")
```

ErrTrustAll reports a trusted-proxy range covering every address, which would let any client choose its own IP.

*Since `v0.1.0`*

## Functions

<a id="Chain"></a>

### func Chain

```go
func Chain(h http.Handler, middlewares ...Middleware) http.Handler
```

Chain wraps h with middlewares. The first middleware is the outermost: it sees the request first and the response last.

*Since `v0.1.0`*

<a id="DefaultCode"></a>

### func DefaultCode

```go
func DefaultCode(status int) string
```

DefaultCode returns the generic code for a status without a mapping, such as "not\_found" for 404.

*Since `v0.1.0`*

<a id="ParseTrustedProxies"></a>

### func ParseTrustedProxies

```go
func ParseTrustedProxies(list string) ([]netip.Prefix, error)
```

ParseTrustedProxies reads a comma-separated list of CIDR ranges or single addresses, such as "10.0.0.0/8, 192.0.2.10". An empty list trusts nothing. It refuses ranges covering every IPv4 or IPv6 address ([ErrTrustAll](#ErrTrustAll)).

*Since `v0.1.0`*

<a id="WriteProblem"></a>

### func WriteProblem

```go
func WriteProblem(w http.ResponseWriter, r *http.Request, p *Problem)
```

WriteProblem writes p as application/problem+json, filling the request ID from the request context when empty.

*Since `v0.1.0`*

## Types

<a id="AccessNote"></a>

### type AccessNote

```go
type AccessNote struct {
	// contains filtered or unexported fields
}
```

AccessNote carries attributes from later middleware and handlers back to [AccessLog](#AccessLog)'s record for the request: the authenticated user, set by the auth middleware, so request logs can be filtered by user. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="AccessNoteFrom"></a>

#### func AccessNoteFrom

```go
func AccessNoteFrom(ctx context.Context) *AccessNote
```

AccessNoteFrom returns the request's note, or nil without [AccessLog](#AccessLog).

*Since `v0.1.0`*

<a id="AccessNote.Add"></a>

#### func (*AccessNote) Add

```go
func (n *AccessNote) Add(attrs ...slog.Attr)
```

Add adds attrs to the request's log record; a nil note ignores them. A key added twice keeps the last value.

*Since `v0.1.0`*

<a id="CORSOptions"></a>
<a id="CORSOptions.AllowedOrigins"></a>
<a id="CORSOptions.AllowedMethods"></a>
<a id="CORSOptions.AllowedHeaders"></a>
<a id="CORSOptions.ExposedHeaders"></a>
<a id="CORSOptions.AllowCredentials"></a>
<a id="CORSOptions.MaxAge"></a>

### type CORSOptions

```go
type CORSOptions struct {
	// AllowedOrigins are exact origins such as "https://app.example.com".
	// Empty disables CORS.
	AllowedOrigins []string
	AllowedMethods []string // default: GET, POST, PUT, PATCH, DELETE
	AllowedHeaders []string // default: Authorization, Content-Type, X-Request-ID
	ExposedHeaders []string // default: X-Request-ID, Retry-After
	// AllowCredentials allows cookies. It can't be combined with a wildcard origin.
	AllowCredentials bool
	MaxAge           time.Duration // preflight cache; default 10 minutes
}
```

CORSOptions configure [CORS](#CORS).

*Since `v0.1.0`*

<a id="Captured"></a>
<a id="Captured.ResponseWriter"></a>

### type Captured

```go
type Captured struct {
	http.ResponseWriter
	// contains filtered or unexported fields
}
```

Captured is a response writer that records what the handler wrote, for middleware that reads the response after the handler returns: its status, whether headers were sent, and the body size. Create one with [Capture](#Capture).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
cw := httpx.Capture(httptest.NewRecorder())
cw.WriteHeader(http.StatusCreated)
_, _ = cw.Write([]byte(`{"id":"bok_1"}`))
fmt.Println(cw.Status(), cw.Bytes(), cw.WroteHeader())
```

Output:

```text
201 14 true
```

<a id="Capture"></a>

#### func Capture

```go
func Capture(w http.ResponseWriter) *Captured
```

Capture returns w wrapped to record the response, or w itself when it is already a \*Captured, so several middlewares share one record. Pass the result to the next handler:

```
cw := httpx.Capture(w)
next.ServeHTTP(cw, r)
if cw.Status() >= 500 { … }
```

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// logErrors logs every response with a 5xx status.
logErrors := func(logger *slog.Logger) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cw := httpx.Capture(w)
			next.ServeHTTP(cw, r)
			if cw.Status() >= 500 {
				logger.ErrorContext(r.Context(), "server error", "status", cw.Status(), "bytes", cw.Bytes())
			}
		})
	}
}

failing := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "database unavailable", http.StatusServiceUnavailable)
})
rec := httptest.NewRecorder()
logErrors(slog.New(slog.DiscardHandler))(failing).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/books", nil))
fmt.Println(rec.Code)
```

Output:

```text
503
```

<a id="Captured.Bytes"></a>

#### func (*Captured) Bytes

```go
func (w *Captured) Bytes() int64
```

Bytes returns the number of body bytes written.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
cw := httpx.Capture(httptest.NewRecorder())
_, _ = fmt.Fprint(cw, "hello, ")
_, _ = fmt.Fprint(cw, "world")
fmt.Println(cw.Bytes())
```

Output:

```text
12
```

<a id="Captured.Status"></a>

#### func (*Captured) Status

```go
func (w *Captured) Status() int
```

Status returns the status the handler sent: 200 when it wrote a body without calling WriteHeader, or wrote nothing at all.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
cw := httpx.Capture(httptest.NewRecorder())
fmt.Println(cw.Status()) // nothing written yet: 200, as net/http sends
cw.WriteHeader(http.StatusNotFound)
fmt.Println(cw.Status())
```

Output:

```text
200
404
```

<a id="Captured.Unwrap"></a>

#### func (*Captured) Unwrap

```go
func (w *Captured) Unwrap() http.ResponseWriter
```

Unwrap supports http.ResponseController (flushing, deadlines).

*Since `v0.2.0 (unreleased)`*

<a id="Captured.Write"></a>

#### func (*Captured) Write

```go
func (w *Captured) Write(b []byte) (int, error)
```

Write records an implicit 200 status and the number of bytes written.

*Since `v0.2.0 (unreleased)`*

<a id="Captured.WriteHeader"></a>

#### func (*Captured) WriteHeader

```go
func (w *Captured) WriteHeader(code int)
```

WriteHeader records the first status code and forwards it.

*Since `v0.2.0 (unreleased)`*

<a id="Captured.WroteHeader"></a>

#### func (*Captured) WroteHeader

```go
func (w *Captured) WroteHeader() bool
```

WroteHeader reports whether the handler has sent the status and headers, after which a middleware can no longer change them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// A middleware that sets a header only while it still can.
addVersion := func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cw := httpx.Capture(w)
		next.ServeHTTP(cw, r)
		if !cw.WroteHeader() {
			cw.Header().Set("X-Version", "1")
			cw.WriteHeader(http.StatusNoContent)
		}
	})
}
rec := httptest.NewRecorder()
addVersion(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/", nil))
fmt.Println(rec.Code, rec.Header().Get("X-Version"))
```

Output:

```text
204 1
```

<a id="FieldError"></a>
<a id="FieldError.Location"></a>
<a id="FieldError.Message"></a>

### type FieldError

```go
type FieldError struct {
	Location string `json:"location,omitempty" doc:"Where the error occurred" example:"body.name"`
	Message  string `json:"message" doc:"What is wrong" example:"expected length >= 1"`
}
```

FieldError describes one invalid input field. It never echoes the submitted value, which may be a password or personal data.

*Since `v0.1.0`*

<a id="MaintenanceOptions"></a>
<a id="MaintenanceOptions.Enabled"></a>
<a id="MaintenanceOptions.Message"></a>
<a id="MaintenanceOptions.RetryAfter"></a>
<a id="MaintenanceOptions.Open"></a>

### type MaintenanceOptions

```go
type MaintenanceOptions struct {
	// Enabled turns maintenance mode on. Nil never turns it on.
	Enabled config.Value[bool]
	// Message is the problem detail clients see; nil or empty sends
	// DefaultMaintenanceMessage.
	Message config.Value[string]
	// RetryAfter is sent in the Retry-After header, in whole seconds; nil or
	// less than a second sends none.
	RetryAfter config.Value[time.Duration]
	// Open are the paths that keep being served while maintenance mode is
	// on. A path ending in a slash covers everything under it, so "/ops/"
	// keeps /ops/settings open; other paths match exactly. Keep health
	// checks open, or load balancers take every instance out of rotation.
	Open []string
}
```

MaintenanceOptions configures [Maintenance](#Maintenance). The values are read on every request, so runtime settings (config.Value) switch maintenance mode on every instance without a restart.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
opts := httpx.MaintenanceOptions{
	Enabled: config.Static(true),
	Message: config.Static("Back at 10:00 UTC"),
	// Health checks exactly, and everything under /ops/.
	Open: []string{"/livez", "/readyz", "/ops/"},
}
rec := httptest.NewRecorder()
httpx.Maintenance(opts)(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/books", nil))
fmt.Println(rec.Code, rec.Header().Get("Content-Type"))
```

Output:

```text
503 application/problem+json
```

<a id="Mapper"></a>

### type Mapper

```go
type Mapper struct {
	// contains filtered or unexported fields
}
```

Mapper turns errors into problems using application-owned mappings. Errors without a mapping become a generic 500 and are logged once with the request ID. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="NewMapper"></a>

#### func NewMapper

```go
func NewMapper(logger *slog.Logger, mappings ...Mapping) (*Mapper, error)
```

NewMapper returns a mapper with the given mappings.

*Since `v0.1.0`*

<a id="Mapper.Add"></a>

#### func (*Mapper) Add

```go
func (m *Mapper) Add(mappings ...Mapping) error
```

Add registers mappings. Each needs a non-nil error, a 4xx or 5xx status and a snake\_case code. Each error is mapped once; several errors may share a code only with the same status, such as "unauthenticated" returned by different modules.

*Since `v0.1.0`*

<a id="Mapper.Match"></a>

#### func (*Mapper) Match

```go
func (m *Mapper) Match(err error) (*Problem, bool)
```

Match returns the problem for err if err is (or wraps) a \*[Problem](#Problem) or a mapped error. It never logs.

*Since `v0.1.0`*

<a id="Mapper.Problem"></a>

#### func (*Mapper) Problem

```go
func (m *Mapper) Problem(ctx context.Context, err error) *Problem
```

Problem returns the problem for err. Unmapped errors become 500 "internal\_error" with a generic detail and are logged.

*Since `v0.1.0`*

<a id="Mapper.Write"></a>

#### func (*Mapper) Write

```go
func (m *Mapper) Write(w http.ResponseWriter, r *http.Request, err error)
```

Write maps err and writes the problem response.

*Since `v0.1.0`*

<a id="Mapping"></a>
<a id="Mapping.Err"></a>
<a id="Mapping.Status"></a>
<a id="Mapping.Code"></a>
<a id="Mapping.Detail"></a>

### type Mapping

```go
type Mapping struct {
	Err    error
	Status int
	Code   string
	Detail string
}
```

A Mapping maps a sentinel error (matched with [errors.Is](https://pkg.go.dev/errors#Is)) to an HTTP status and stable code. Detail defaults to the error's message.

*Since `v0.1.0`*

<a id="Middleware"></a>

### type Middleware

```go
type Middleware func(http.Handler) http.Handler
```

Middleware wraps an http.Handler.

*Since `v0.1.0`*

<a id="AccessLog"></a>

#### func AccessLog

```go
func AccessLog(logger *slog.Logger) Middleware
```

AccessLog logs one line per request: method, path, route pattern, status, duration, response size, request ID and what later middleware noted with [AccessNoteFrom](#AccessNoteFrom) (the authenticated user). Query strings and bodies are never logged.

*Since `v0.1.0`*

<a id="BodyLimit"></a>

#### func BodyLimit

```go
func BodyLimit(n int64) Middleware
```

BodyLimit rejects request bodies larger than n bytes with a 413 problem. Bodies without a declared length are cut off at n bytes while reading.

*Since `v0.1.0`*

<a id="CORS"></a>

#### func CORS

```go
func CORS(opts CORSOptions) (Middleware, error)
```

CORS returns middleware allowing cross-origin requests from an explicit allowlist. A "\*" origin is rejected when credentials are allowed.

*Since `v0.1.0`*

<a id="CrossOrigin"></a>

#### func CrossOrigin

```go
func CrossOrigin(trustedOrigins ...string) (Middleware, error)
```

CrossOrigin protects against cross-site request forgery using the browser's Sec-Fetch-Site and Origin headers ([http.CrossOriginProtection](https://pkg.go.dev/net/http#CrossOriginProtection)). Non-browser clients, which send neither header, are allowed. Denied requests receive a 403 problem with code "cross\_origin\_request\_denied".

*Since `v0.1.0`*

<a id="Maintenance"></a>

#### func Maintenance

```go
func Maintenance(opts MaintenanceOptions) Middleware
```

Maintenance answers every request with 503 and the problem code maintenance while opts.Enabled is on, except requests for opts.Open paths (ADR-0051). It reads the values from opts on each request and does no other work, so it costs no database query when the values are runtime settings.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "books") })
// In an app, Enabled, Message and RetryAfter are runtime settings, so
// operators switch every instance without a restart.
h := httpx.Maintenance(httpx.MaintenanceOptions{
	Enabled:    config.Static(true),
	RetryAfter: config.Static(5 * time.Minute),
	Open:       []string{"/livez", "/readyz"},
})(books)

for _, path := range []string{"/v1/books", "/readyz"} {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	fmt.Println(path, rec.Code, rec.Header().Get("Retry-After"))
}
```

Output:

```text
/v1/books 503 300
/readyz 200
```

<a id="Recover"></a>

#### func Recover

```go
func Recover(logger *slog.Logger) Middleware
```

Recover turns a panic into a logged error and a 500 problem response. [http.ErrAbortHandler](https://pkg.go.dev/net/http#ErrAbortHandler) panics are re-raised, as net/http expects.

*Since `v0.1.0`*

<a id="RequestID"></a>

#### func RequestID

```go
func RequestID() Middleware
```

RequestID generates a request ID, stores it in the request context and echoes it in the response header. It ignores incoming X-Request-ID headers, so clients can't give their requests another request's ID in logs, audit events and jobs; use [RequestIDFrom](#RequestIDFrom) to accept them from trusted callers.

*Since `v0.1.0`*

<a id="RequestIDFrom"></a>

#### func RequestIDFrom

```go
func RequestIDFrom(trusted []netip.Prefix) Middleware
```

RequestIDFrom is [RequestID](#RequestID) that accepts a valid incoming X-Request-ID from requests whose client address is in trusted: gateways and internal services that assign request IDs. Match addresses as this middleware sees them, after [TrustedProxies](#TrustedProxies). Other requests get a generated ID.

*Since `v0.1.0`*

<a id="SecureHeaders"></a>

#### func SecureHeaders

```go
func SecureHeaders(opts SecureHeadersOptions) Middleware
```

SecureHeaders sets security headers suitable for a JSON API. HTML routes such as /docs set their own Content-Security-Policy.

*Since `v0.1.0`*

<a id="TrustedProxies"></a>

#### func TrustedProxies

```go
func TrustedProxies(trusted []netip.Prefix) Middleware
```

TrustedProxies sets each request's RemoteAddr to its client's address when it arrives through trusted proxies (ADR-0052). For a request whose peer is in trusted, it walks X-Forwarded-For from the right, skips trusted addresses, and uses the first untrusted one. Requests from other peers keep their address and their forwarding headers are ignored, so clients can't choose their IP. With no trusted ranges it changes nothing.

Install it first, after recovery, so logs, audit events and rate limits see the client.

*Since `v0.1.0`*

<a id="Problem"></a>
<a id="Problem.Type"></a>
<a id="Problem.Title"></a>
<a id="Problem.Status"></a>
<a id="Problem.Code"></a>
<a id="Problem.Detail"></a>
<a id="Problem.RequestID"></a>
<a id="Problem.Errors"></a>

### type Problem

```go
type Problem struct {
	Type      string       `json:"type,omitempty" doc:"URI identifying the problem type"`
	Title     string       `json:"title" doc:"Short summary of the problem type" example:"Conflict"`
	Status    int          `json:"status" doc:"HTTP status code" example:"409"`
	Code      string       `json:"code" doc:"Stable machine-readable error code" example:"project_name_taken"`
	Detail    string       `json:"detail,omitempty" doc:"Human-readable explanation" example:"project name is already taken"`
	RequestID string       `json:"request_id,omitempty" doc:"Correlates with server logs and traces" example:"req_9f86d081884c7d65"`
	Errors    []FieldError `json:"errors,omitempty" doc:"Field-level validation errors"`
}
```

Problem is an RFC 9457 problem details response extended with a stable machine-readable code and the request ID. Codes are public API; titles and details are not (ADR-0015).

*Since `v0.1.0`*

<a id="NewProblem"></a>

#### func NewProblem

```go
func NewProblem(status int, code, detail string) *Problem
```

NewProblem returns a problem with the standard title for status.

*Since `v0.1.0`*

<a id="Problem.ContentType"></a>

#### func (*Problem) ContentType

```go
func (p *Problem) ContentType(ct string) string
```

ContentType returns application/problem+json for JSON responses.

*Since `v0.1.0`*

<a id="Problem.Error"></a>

#### func (*Problem) Error

```go
func (p *Problem) Error() string
```

Error returns the detail, or the title when detail is empty.

*Since `v0.1.0`*

<a id="Problem.GetStatus"></a>

#### func (*Problem) GetStatus

```go
func (p *Problem) GetStatus() int
```

GetStatus returns the HTTP status. It lets frameworks such as Huma use a Problem as a status error.

*Since `v0.1.0`*

<a id="SecureHeadersOptions"></a>
<a id="SecureHeadersOptions.HSTSMaxAge"></a>

### type SecureHeadersOptions

```go
type SecureHeadersOptions struct {
	// HSTSMaxAge enables Strict-Transport-Security when positive. Enable it
	// only when the app is served exclusively over HTTPS.
	HSTSMaxAge time.Duration
}
```

SecureHeadersOptions configure [SecureHeaders](#SecureHeaders).

*Since `v0.1.0`*

<a id="Server"></a>

### type Server

```go
type Server struct {
	// contains filtered or unexported fields
}
```

Server is an HTTP server that implements app.Runner.

*Since `v0.1.0`*

<a id="NewServer"></a>

#### func NewServer

```go
func NewServer(addr string, handler http.Handler, opts ...ServerOption) *Server
```

NewServer returns a server for handler listening on addr.

*Since `v0.1.0`*

<a id="Server.Addr"></a>

#### func (*Server) Addr

```go
func (s *Server) Addr(ctx context.Context) (string, error)
```

Addr returns the listening address once [Server.Run](#Server.Run) has started listening, blocking until then or until ctx is done. It is useful with port 0 in tests.

*Since `v0.1.0`*

<a id="Server.Run"></a>

#### func (*Server) Run

```go
func (s *Server) Run(ctx context.Context) error
```

Run listens and serves until ctx is done, then shuts down gracefully: it stops accepting connections and waits for in-flight requests up to the shutdown timeout. Run returns nil after a graceful shutdown.

*Since `v0.1.0`*

<a id="ServerOption"></a>

### type ServerOption

```go
type ServerOption interface {
	// contains filtered or unexported methods
}
```

A ServerOption configures a [Server](#Server).

*Since `v0.1.0`*

<a id="WithErrorLogger"></a>

#### func WithErrorLogger

```go
func WithErrorLogger(logger *slog.Logger) ServerOption
```

WithErrorLogger routes the server's internal errors (for example TLS handshake failures) to logger.

*Since `v0.1.0`*

<a id="WithShutdownTimeout"></a>

#### func WithShutdownTimeout

```go
func WithShutdownTimeout(d time.Duration) ServerOption
```

WithShutdownTimeout sets how long graceful shutdown may take. Default: [DefaultShutdownTimeout](#DefaultShutdownTimeout).

*Since `v0.1.0`*

<a id="WithTimeouts"></a>

#### func WithTimeouts

```go
func WithTimeouts(readHeader, read, write, idle time.Duration) ServerOption
```

WithTimeouts overrides read-header, read, write and idle timeouts. Zero values keep the defaults.

*Since `v0.1.0`*
