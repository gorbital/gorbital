package telemetry

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Limits of the handler returned by [Telemetry.MetricsHandler]. A scrape
// collects every metric, so concurrent scrapes beyond a few are refused
// with 503 instead of queueing, and a slow one is cut off.
const (
	metricsMaxScrapes    = 4
	metricsScrapeTimeout = 10 * time.Second
)

// WithPrometheus enables a Prometheus exporter, alongside OTLP export when
// both are on: [Telemetry.MetricsHandler] then serves every metric of the
// meter provider in the Prometheus text format (ADR-0063). The exporter uses
// its own registry, never Prometheus's global one. Default: off.
func WithPrometheus(enabled bool) Option {
	return optionFunc(func(o *options) { o.prometheus = enabled })
}

// WithRuntimeMetrics records Go runtime metrics (memory, garbage collector
// goal, goroutines, GOMAXPROCS) with the OpenTelemetry runtime
// instrumentation. They are read from runtime/metrics when metrics are
// collected, so they cost nothing while no exporter is on. Default: off.
func WithRuntimeMetrics() Option {
	return optionFunc(func(o *options) { o.runtimeMetrics = true })
}

// MetricsHandler returns the Prometheus scrape handler enabled by
// [WithPrometheus], or a handler answering 404 when it's off. Serve it on a
// separate listener bound to a private interface, never on the API's public
// address: it has no authentication, and metrics describe the service's
// routes and load.
//
// Scrapes collect on demand. At most four run at once (others get 503), each
// for up to 10 seconds; collection errors are logged and the metrics that
// could be collected are still served.
func (t *Telemetry) MetricsHandler() http.Handler {
	if t.metrics == nil {
		return http.NotFoundHandler()
	}
	return t.metrics
}

// newPrometheus returns a reader exporting to a new registry and the handler
// serving that registry.
func newPrometheus(logger *slog.Logger) (sdkmetric.Reader, http.Handler, error) {
	reg := prometheus.NewRegistry()
	exp, err := otelprometheus.New(otelprometheus.WithRegisterer(reg))
	if err != nil {
		return nil, nil, fmt.Errorf("telemetry: prometheus exporter: %w", err)
	}
	h := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog:            slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
		ErrorHandling:       promhttp.ContinueOnError,
		MaxRequestsInFlight: metricsMaxScrapes,
		Timeout:             metricsScrapeTimeout,
	})
	return exp, h, nil
}

// startRuntimeMetrics registers the Go runtime instruments on mp.
func startRuntimeMetrics(mp *sdkmetric.MeterProvider) error {
	if err := otelruntime.Start(otelruntime.WithMeterProvider(mp)); err != nil {
		return fmt.Errorf("telemetry: runtime metrics: %w", err)
	}
	return nil
}
