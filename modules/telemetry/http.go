package telemetry

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware starts a server span for each request, extracts incoming
// W3C trace context, records standard HTTP server metrics, and names the span
// after the matched http.ServeMux pattern (for example
// "GET /v1/projects/{id}") so traces group by route, not by raw path.
func (t *Telemetry) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		named := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			// http.ServeMux sets r.Pattern on the request it routes.
			if r.Pattern != "" {
				span := trace.SpanFromContext(r.Context())
				span.SetName(r.Pattern)
				span.SetAttributes(attribute.String("http.route", r.Pattern))
			}
		})
		return otelhttp.NewHandler(named, "http.server",
			otelhttp.WithTracerProvider(t.tp),
			otelhttp.WithMeterProvider(t.mp),
			otelhttp.WithPropagators(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})),
		)
	}
}
