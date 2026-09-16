package devconsole

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/requestid"
)

const (
	// maxPathLength bounds a recorded request path.
	maxPathLength = 512
	// otherMethod replaces request methods that aren't standard HTTP
	// methods.
	otherMethod = "_OTHER"
)

// Request is one finished HTTP request. It never holds the query string,
// headers, cookies or bodies.
type Request struct {
	// Time is when the request finished.
	Time   time.Time `json:"time"`
	Method string    `json:"method"`
	// Route is the pattern the router matched, such as "/v1/projects/{id}";
	// empty when no route matched.
	Route string `json:"route"`
	// Path is the request path, without the query string.
	Path       string  `json:"path"`
	Status     int     `json:"status"`
	DurationMS float64 `json:"duration_ms"`
	RequestID  string  `json:"request_id,omitempty"`
	TraceID    string  `json:"trace_id,omitempty"`
}

// RecordRequest adds a finished request to the console's recent requests
// and live streams. Apps with a request collector subscribe it (see
// gorbital.dev/modules/observability's Collector.Subscribe); others use
// [Console.Middleware]. It never blocks: a slow stream client misses
// requests instead.
func (c *Console) RecordRequest(r Request) {
	if c == nil {
		return
	}
	if path, _, cut := strings.Cut(r.Path, "?"); cut {
		r.Path = path // defensive: paths never carry the query
	}
	r.Path = truncate(r.Path, maxPathLength)
	r.Route = truncate(r.Route, maxPathLength)
	if !slices.Contains(standardMethods, r.Method) {
		r.Method = otherMethod
	}
	r.RequestID = truncate(r.RequestID, 128)
	r.TraceID = truncate(r.TraceID, 64)
	c.requests.add(r)
}

// standardMethods are the methods recorded as they are.
var standardMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace,
}

// Middleware records every request with [Console.RecordRequest], for apps
// without a request collector. Install it early, after panic recovery, and
// wrap the router with [RecordRoute] so the route pattern is known even
// when middleware in between copies the request. A nil console returns a
// middleware that does nothing.
func (c *Console) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if c == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w}
			inner := r.WithContext(context.WithValue(r.Context(), recorderKey{}, rec))
			completed := false
			defer func() {
				status := rec.status
				switch {
				case !completed:
					status = http.StatusInternalServerError // panicking
				case status == 0:
					status = http.StatusOK
				}
				if rec.route == "" {
					rec.route = routeOf(inner.Pattern)
				}
				req := Request{
					Time: time.Now(), Method: r.Method, Route: rec.route, Path: r.URL.Path, Status: status,
					DurationMS: float64(time.Since(start).Microseconds()) / 1000,
					RequestID:  requestid.From(inner.Context()),
				}
				if sc := trace.SpanContextFromContext(inner.Context()); sc.HasTraceID() {
					req.TraceID = sc.TraceID().String()
				}
				c.RecordRequest(req)
			}()
			next.ServeHTTP(rec, inner)
			completed = true
		})
	}
}

// RecordRoute wraps the application's router so [Console.Middleware]
// learns the route pattern it matched. Without the middleware it does
// nothing.
func RecordRoute(router http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rec, ok := r.Context().Value(recorderKey{}).(*recorder); ok {
			defer func() {
				if rec.route == "" {
					rec.route = routeOf(r.Pattern)
				}
			}()
		}
		router.ServeHTTP(w, r)
	})
}

// routeOf returns the path of a ServeMux pattern such as
// "GET example.com/v1/items/{id}": "/v1/items/{id}".
func routeOf(pattern string) string {
	if _, rest, ok := strings.Cut(pattern, " "); ok {
		pattern = rest
	}
	if i := strings.IndexByte(pattern, '/'); i > 0 {
		pattern = pattern[i:]
	}
	return pattern
}

type recorderKey struct{}

// recorder captures a request's status and route.
type recorder struct {
	http.ResponseWriter
	status int
	route  string
}

// WriteHeader records the first final status code.
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 && code >= 200 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write records an implicit 200 status.
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap supports http.ResponseController (flushing, deadlines).
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
