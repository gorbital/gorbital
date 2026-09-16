package observability

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/requestid"
)

// Middleware records every request in the collector: its method, the route
// pattern the router matched, its status and duration. Install it early in
// the chain, after panic recovery, so responses written by other
// middleware (rate limits, maintenance mode, authentication failures) are
// counted too; a handler that panics counts as a 500.
//
// Paths, query strings, headers and other values clients choose never
// become part of a series: the route is the pattern registered in code, or
// empty when none matched. http.ServeMux sets the pattern on the request it
// routes, which is a copy when a middleware in between calls r.WithContext,
// so wrap the router with [RecordRoute]:
//
//	handler := httpx.Chain(observability.RecordRoute(mux), httpx.Recover(logger), collector.Middleware(), …)
func (c *Collector) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
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
					rec.route = routeOf(inner.Pattern) // no middleware copied the request
				}
				req := Request{Time: c.now(), Method: r.Method, Route: rec.route, Status: status, Duration: time.Since(start)}
				if c.hasSubscribers() {
					req.Path, req.RequestID = r.URL.Path, requestid.From(inner.Context())
					if sc := trace.SpanContextFromContext(inner.Context()); sc.HasTraceID() {
						req.TraceID = sc.TraceID().String()
					}
				}
				c.Record(req)
			}()
			next.ServeHTTP(rec, inner)
			completed = true
		})
	}
}

// RecordRoute wraps the application's router, normally the http.ServeMux at
// the end of the middleware chain, so [Collector.Middleware] learns the
// route pattern the router matched even when middleware between them
// replaced the request. Without the middleware, it does nothing.
func RecordRoute(router http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rec, ok := r.Context().Value(recorderKey{}).(*recorder); ok {
			defer func() { // also when the handler panics
				if rec.route == "" {
					rec.route = routeOf(r.Pattern)
				}
			}()
		}
		router.ServeHTTP(w, r)
	})
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
