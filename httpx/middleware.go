package httpx

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/requestid"
)

// Middleware wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain wraps h with middlewares. The first middleware is the outermost: it
// sees the request first and the response last.
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// RequestID generates a request ID, stores it in the request context and
// echoes it in the response header. It ignores incoming X-Request-ID headers,
// so clients can't give their requests another request's ID in logs, audit
// events and jobs; use [RequestIDFrom] to accept them from trusted callers.
func RequestID() Middleware { return RequestIDFrom(nil) }

// RequestIDFrom is [RequestID] that accepts a valid incoming X-Request-ID
// from requests whose client address is in trusted: gateways and internal
// services that assign request IDs. Match addresses as this middleware sees
// them, after [TrustedProxies]. Other requests get a generated ID.
func RequestIDFrom(trusted []netip.Prefix) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestid.Header)
			if !requestid.Valid(id) || !fromTrusted(r, trusted) {
				id = requestid.New()
			}
			w.Header().Set(requestid.Header, id)
			next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
		})
	}
}

// Recover turns a panic into a logged error and a 500 problem response.
// [http.ErrAbortHandler] panics are re-raised, as net/http expects.
func Recover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := wrap(w)
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(v)
				}
				logger.ErrorContext(r.Context(), "panic recovered",
					"panic", fmt.Sprint(v),
					"stack", string(debug.Stack()),
					"request_id", requestid.From(r.Context()),
				)
				if !rw.wroteHeader {
					WriteProblem(rw, r, NewProblem(http.StatusInternalServerError, "internal_error", "an internal error occurred"))
				}
			}()
			next.ServeHTTP(rw, r)
		})
	}
}

// SecureHeadersOptions configure [SecureHeaders].
type SecureHeadersOptions struct {
	// HSTSMaxAge enables Strict-Transport-Security when positive. Enable it
	// only when the app is served exclusively over HTTPS.
	HSTSMaxAge time.Duration
}

// SecureHeaders sets security headers suitable for a JSON API. HTML routes
// such as /docs set their own Content-Security-Policy.
func SecureHeaders(opts SecureHeadersOptions) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			if opts.HSTSMaxAge > 0 {
				h.Set("Strict-Transport-Security", "max-age="+strconv.Itoa(int(opts.HSTSMaxAge.Seconds()))+"; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORSOptions configure [CORS].
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

// CORS returns middleware allowing cross-origin requests from an explicit
// allowlist. A "*" origin is rejected when credentials are allowed.
func CORS(opts CORSOptions) (Middleware, error) {
	if opts.AllowCredentials && slices.Contains(opts.AllowedOrigins, "*") {
		return nil, errors.New("httpx: CORS wildcard origin can't be combined with credentials")
	}
	for _, o := range opts.AllowedOrigins {
		if o != "*" && !validOrigin(o) {
			return nil, fmt.Errorf("httpx: CORS origin %q must be scheme://host[:port] without a trailing slash", o)
		}
	}
	methods := strings.Join(orDefault(opts.AllowedMethods, []string{"GET", "POST", "PUT", "PATCH", "DELETE"}), ", ")
	headers := strings.Join(orDefault(opts.AllowedHeaders, []string{"Authorization", "Content-Type", requestid.Header}), ", ")
	exposed := strings.Join(orDefault(opts.ExposedHeaders, []string{requestid.Header, "Retry-After"}), ", ")
	maxAge := opts.MaxAge
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			h := w.Header()
			h.Add("Vary", "Origin")
			allowed := origin != "" && (slices.Contains(opts.AllowedOrigins, origin) || slices.Contains(opts.AllowedOrigins, "*"))
			if !allowed {
				next.ServeHTTP(w, r)
				return
			}
			h.Set("Access-Control-Allow-Origin", origin)
			if opts.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				h.Set("Access-Control-Allow-Methods", methods)
				h.Set("Access-Control-Allow-Headers", headers)
				h.Set("Access-Control-Max-Age", strconv.Itoa(int(maxAge.Seconds())))
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.Set("Access-Control-Expose-Headers", exposed)
			next.ServeHTTP(w, r)
		})
	}, nil
}

// validOrigin reports whether origin is scheme://host[:port], with an http
// or https scheme and nothing else.
func validOrigin(origin string) bool {
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" &&
		u.User == nil && u.Opaque == "" && u.RawPath == "" && u.Path == "" && !u.ForceQuery && u.RawQuery == "" && u.Fragment == "" &&
		u.Scheme+"://"+u.Host == origin
}

func orDefault(v, def []string) []string {
	if len(v) == 0 {
		return def
	}
	return v
}

// CrossOrigin protects against cross-site request forgery using the
// browser's Sec-Fetch-Site and Origin headers ([http.CrossOriginProtection]).
// Non-browser clients, which send neither header, are allowed. Denied
// requests receive a 403 problem with code "cross_origin_request_denied".
func CrossOrigin(trustedOrigins ...string) (Middleware, error) {
	p := http.NewCrossOriginProtection()
	for _, o := range trustedOrigins {
		if err := p.AddTrustedOrigin(o); err != nil {
			return nil, fmt.Errorf("httpx: trusted origin %q: %w", o, err)
		}
	}
	p.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteProblem(w, r, NewProblem(http.StatusForbidden, "cross_origin_request_denied", "cross-origin request denied"))
	}))
	return p.Handler, nil
}

// BodyLimit rejects request bodies larger than n bytes with a 413 problem.
// Bodies without a declared length are cut off at n bytes while reading.
func BodyLimit(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				WriteProblem(w, r, NewProblem(http.StatusRequestEntityTooLarge, "request_too_large",
					fmt.Sprintf("request body must not exceed %d bytes", n)))
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// AccessLog logs one line per request: method, route pattern, status,
// duration, response size and request ID. Query strings and bodies are never
// logged.
func AccessLog(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := wrap(w)
			next.ServeHTTP(rw, r)

			route := r.Pattern
			if route == "" {
				route = r.URL.Path
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.Int("status", rw.status()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.Int64("bytes", rw.bytes),
				slog.String("request_id", requestid.From(r.Context())),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	code        int
	bytes       int64
	wroteHeader bool
}

func wrap(w http.ResponseWriter) *responseWriter {
	if rw, ok := w.(*responseWriter); ok {
		return rw
	}
	return &responseWriter{ResponseWriter: w}
}

// WriteHeader records the first status code and forwards it.
func (w *responseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.code, w.wroteHeader = code, true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Write records an implicit 200 status and the number of bytes written.
func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.code, w.wroteHeader = http.StatusOK, true
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Unwrap supports http.ResponseController (flushing, deadlines).
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) status() int {
	if w.code == 0 {
		return http.StatusOK
	}
	return w.code
}
