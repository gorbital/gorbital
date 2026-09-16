package telemetry

import (
	"net"
	"net/http"
	"net/netip"

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
// which come from the client's Host header.
func (t *Telemetry) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		named := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			span := trace.SpanFromContext(r.Context())
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				span.SetAttributes(attribute.String("client.address", host))
			}
			next.ServeHTTP(w, r)
			// http.ServeMux sets r.Pattern on the request it routes.
			if r.Pattern != "" {
				span.SetName(r.Pattern)
				span.SetAttributes(attribute.String("http.route", r.Pattern))
			}
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
