// Package telemetry sets up OpenTelemetry tracing and metrics and structured
// logs correlated with requests and traces (ADR-0007).
//
// Tracing is always on, so every log line written with a request context
// carries trace and span IDs. Spans and metrics are exported only when OTLP
// export is enabled; the exporters read the standard OTEL_EXPORTER_OTLP_*
// environment variables, as OpenTelemetry specifies.
//
// By default [Setup] installs its providers and the W3C trace-context
// propagator as OpenTelemetry globals, which instrumentation libraries use.
// Call it once, from the composition root.
//
// Stability: pre-1.0 (ADR-0015).
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// LogFormat selects the log encoding.
type LogFormat string

// Log formats.
const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// Telemetry holds the configured logger and OpenTelemetry providers.
type Telemetry struct {
	logger *slog.Logger
	tp     *sdktrace.TracerProvider
	mp     *sdkmetric.MeterProvider
}

// Logger returns the correlated structured logger.
func (t *Telemetry) Logger() *slog.Logger { return t.logger }

// TracerProvider returns the tracer provider.
func (t *Telemetry) TracerProvider() trace.TracerProvider { return t.tp }

// MeterProvider returns the meter provider.
func (t *Telemetry) MeterProvider() metric.MeterProvider { return t.mp }

// Shutdown flushes and stops exporters. Register it first on the cleanup
// stack so it runs last.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	err := errors.Join(t.tp.Shutdown(ctx), t.mp.Shutdown(ctx))
	if err != nil {
		return fmt.Errorf("telemetry: shutdown: %w", err)
	}
	return nil
}

type options struct {
	exportOTLP   bool
	spanExporter sdktrace.SpanExporter
	logWriter    io.Writer
	logFormat    LogFormat
	logLevel     slog.Leveler
	sampleRatio  float64
	setGlobals   bool
}

// An Option configures [Setup].
type Option interface{ apply(*options) }

type optionFunc func(*options)

func (f optionFunc) apply(o *options) { f(o) }

// WithOTLPExport enables exporting spans and metrics over OTLP/HTTP,
// configured by the standard OTEL_EXPORTER_OTLP_* environment variables.
func WithOTLPExport(enabled bool) Option {
	return optionFunc(func(o *options) { o.exportOTLP = enabled })
}

// WithSpanExporter adds a synchronous span exporter, for tests and custom
// backends.
func WithSpanExporter(e sdktrace.SpanExporter) Option {
	return optionFunc(func(o *options) { o.spanExporter = e })
}

// WithLogWriter sets where logs are written. Default: os.Stdout.
func WithLogWriter(w io.Writer) Option {
	return optionFunc(func(o *options) { o.logWriter = w })
}

// WithLogFormat sets JSON (default, for production) or text (for local
// development) logs.
func WithLogFormat(f LogFormat) Option {
	return optionFunc(func(o *options) { o.logFormat = f })
}

// WithLogLevel sets the minimum log level. Default: info.
func WithLogLevel(l slog.Leveler) Option {
	return optionFunc(func(o *options) { o.logLevel = l })
}

// WithSampleRatio sets the fraction of new traces sampled, from 0 to 1.
// Child spans follow their parent's decision. Default: 1.
func WithSampleRatio(r float64) Option {
	return optionFunc(func(o *options) { o.sampleRatio = r })
}

// WithoutGlobals keeps Setup from installing OpenTelemetry globals.
func WithoutGlobals() Option {
	return optionFunc(func(o *options) { o.setGlobals = false })
}

// Setup configures telemetry for service at version.
func Setup(ctx context.Context, service, version string, opts ...Option) (*Telemetry, error) {
	o := options{logWriter: os.Stdout, logFormat: LogFormatJSON, logLevel: slog.LevelInfo, sampleRatio: 1, setGlobals: true}
	for _, opt := range opts {
		opt.apply(&o)
	}
	switch {
	case service == "":
		return nil, errors.New("telemetry: service name is required")
	case o.sampleRatio < 0 || o.sampleRatio > 1:
		return nil, fmt.Errorf("telemetry: sample ratio %v must be between 0 and 1", o.sampleRatio)
	case o.logFormat != LogFormatJSON && o.logFormat != LogFormatText:
		return nil, fmt.Errorf("telemetry: unknown log format %q", o.logFormat)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("service.name", service), attribute.String("service.version", version)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(o.sampleRatio))),
	}
	mpOpts := []sdkmetric.Option{sdkmetric.WithResource(res)}
	if o.exportOTLP {
		traceExp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("telemetry: trace exporter: %w", err)
		}
		metricExp, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("telemetry: metric exporter: %w", err), traceExp.Shutdown(ctx))
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(traceExp))
		mpOpts = append(mpOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)))
	}
	if o.spanExporter != nil {
		tpOpts = append(tpOpts, sdktrace.WithSyncer(o.spanExporter))
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)
	mp := sdkmetric.NewMeterProvider(mpOpts...)

	if o.setGlobals {
		otel.SetTracerProvider(tp)
		otel.SetMeterProvider(mp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	}

	hopts := &slog.HandlerOptions{Level: o.logLevel}
	var base slog.Handler
	if o.logFormat == LogFormatText {
		base = slog.NewTextHandler(o.logWriter, hopts)
	} else {
		base = slog.NewJSONHandler(o.logWriter, hopts)
	}
	base = base.WithAttrs([]slog.Attr{slog.String("service", service)})

	return &Telemetry{logger: slog.New(NewLogHandler(base)), tp: tp, mp: mp}, nil
}
