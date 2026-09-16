package telemetry

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware starts a server span for each request, records standard
// HTTP server metrics, and names the span after the matched http.ServeMux
// pattern (for example "GET /v1/projects/{id}") so traces group by route,
// not by raw path.
//
// Incoming W3C trace context is untrusted by default: each request starts a
// new trace, sampled by [WithSampleRatio], that links to the caller's span,
// and incoming baggage is ignored. A client can't hide its requests from
// tracing, force sampling, or join another request's trace. Requests from
// the callers given to [WithTraceContextFrom] continue their trace instead.
//
// The span's client.address is the request's RemoteAddr, as resolved by
// httpx.TrustedProxies when installed before this middleware, never a raw
// X-Forwarded-For header. Metrics leave out server.address and server.port,
// which come from the client's Host header, and carry http.route, the
// matched pattern without its method, instead of the path.
//
// http.ServeMux sets the pattern on the request it routes, which is a copy
// when a middleware between this one and the mux calls r.WithContext. Wrap
// the mux with [RecordRoute] so the route reaches spans and metrics anyway.
func (t *Telemetry) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		named := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				span.SetAttributes(attribute.String("client.address", host))
			}
			rec := &routeRecorder{span: span}
			rec.labeler, _ = otelhttp.LabelerFromContext(r.Context())
			inner := r.WithContext(context.WithValue(r.Context(), routeRecorderKey{}, rec))
			next.ServeHTTP(w, inner)
			rec.record(inner.Pattern) // when no middleware copied the request
		})
		public := otelhttp.NewHandler(named, "http.server",
			otelhttp.WithTracerProvider(t.tp),
			otelhttp.WithMeterProvider(t.mp),
			// Only read to link the caller's span; baggage is never extracted.
			otelhttp.WithPropagators(propagation.TraceContext{}),
			otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		)
		if len(t.traceCallers) == 0 {
			return public
		}
		trusted := otelhttp.NewHandler(named, "http.server",
			otelhttp.WithTracerProvider(t.tp),
			otelhttp.WithMeterProvider(t.mp),
			otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})),
		)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if t.trustedCaller(r) {
				trusted.ServeHTTP(w, r)
				return
			}
			public.ServeHTTP(w, r)
		})
	}
}

// trustedCaller reports whether r's client address is in the callers given
// to [WithTraceContextFrom].
func (t *Telemetry) trustedCaller(r *http.Request) bool {
	addr, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	client := addr.Addr().Unmap()
	for _, p := range t.traceCallers {
		if p.Contains(client) {
			return true
		}
	}
	return false
}

// RecordRoute wraps the application's router, normally the http.ServeMux
// at the end of the middleware chain, so [Telemetry.HTTPMiddleware] names
// the span and labels HTTP metrics with the route pattern the router
// matched, even when middleware between them replaced the request with
// r.WithContext:
//
//	handler := httpx.Chain(telemetry.RecordRoute(mux), tel.HTTPMiddleware(), auth, …)
//
// Routes are the patterns registered in code, so they keep metric series
// bounded whatever paths clients request. Without HTTPMiddleware, RecordRoute
// does nothing.
func RecordRoute(router http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r)
		if rec, ok := r.Context().Value(routeRecorderKey{}).(*routeRecorder); ok {
			rec.record(r.Pattern)
		}
	})
}

type routeRecorderKey struct{}

// routeRecorder carries a request's route from [RecordRoute] back to
// [Telemetry.HTTPMiddleware]. The first non-empty pattern wins.
type routeRecorder struct {
	span    trace.Span
	labeler *otelhttp.Labeler // nil when otelhttp didn't add one
	once    sync.Once
}

func (rr *routeRecorder) record(pattern string) {
	if pattern == "" {
		return
	}
	rr.once.Do(func() {
		rr.span.SetName(pattern)
		rr.span.SetAttributes(attribute.String("http.route", pattern))
		// Metrics use the path part, as otelhttp does: "GET /v1/items/{id}"
		// becomes "/v1/items/{id}".
		if i := strings.IndexByte(pattern, '/'); i >= 0 && rr.labeler != nil {
			rr.labeler.Add(attribute.String("http.route", pattern[i:]))
		}
	})
}
