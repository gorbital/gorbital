# modules/telemetry

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/telemetry"
```

Package telemetry sets up OpenTelemetry tracing and metrics and structured logs correlated with requests and traces (ADR-0007).

Tracing is always on, so every log line written with a request context carries trace and span IDs. Spans and metrics are exported only when OTLP export is enabled; the exporters read the standard OTEL\_EXPORTER\_OTLP\_\* environment variables, as OpenTelemetry specifies. Metrics can also be scraped in the Prometheus format from [Telemetry.MetricsHandler](#Telemetry.MetricsHandler) (ADR-0063).

By default [Setup](#Setup) installs its providers and the W3C trace-context propagator as OpenTelemetry globals, which instrumentation libraries use. Call it once, from the composition root.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`KeyRequestID`](#KeyRequestID), [`KeyTraceID`](#KeyTraceID), [`KeySpanID`](#KeySpanID), [`KeyOrgID`](#KeyOrgID)
- Functions: [`NewLogHandler`](#NewLogHandler), [`RecordRoute`](#RecordRoute)
- Types:
  - [`LogFormat`](#LogFormat): [`LogFormatJSON`](#LogFormatJSON), [`LogFormatText`](#LogFormatText)
  - [`Option`](#Option): [`WithLogFormat`](#WithLogFormat), [`WithLogLevel`](#WithLogLevel), [`WithLogTee`](#WithLogTee), [`WithLogWriter`](#WithLogWriter), [`WithOTLPExport`](#WithOTLPExport), [`WithPrometheus`](#WithPrometheus), [`WithRuntimeMetrics`](#WithRuntimeMetrics), [`WithSampleRatio`](#WithSampleRatio), [`WithSpanExporter`](#WithSpanExporter), [`WithTraceContextFrom`](#WithTraceContextFrom), [`WithoutGlobals`](#WithoutGlobals)
  - [`Telemetry`](#Telemetry): [`Setup`](#Setup), [`Telemetry.HTTPMiddleware`](#Telemetry.HTTPMiddleware), [`Telemetry.Logger`](#Telemetry.Logger), [`Telemetry.MeterProvider`](#Telemetry.MeterProvider), [`Telemetry.MetricsHandler`](#Telemetry.MetricsHandler), [`Telemetry.Shutdown`](#Telemetry.Shutdown), [`Telemetry.TracerProvider`](#Telemetry.TracerProvider)

## Constants

<a id="KeyRequestID"></a>
<a id="KeyTraceID"></a>
<a id="KeySpanID"></a>
<a id="KeyOrgID"></a>

```go
const (
	KeyRequestID = "request_id"
	KeyTraceID   = "trace_id"
	KeySpanID    = "span_id"
	KeyOrgID     = "org_id"
)
```

Log attribute keys added from the context.

*Since `v0.1.0`*

## Functions

<a id="NewLogHandler"></a>

### func NewLogHandler

```go
func NewLogHandler(h slog.Handler) slog.Handler
```

NewLogHandler wraps h so records logged with a context carry request\_id, trace\_id, span\_id and org\_id when available. Keys already present on the record are not added twice.

*Since `v0.1.0`*

<a id="RecordRoute"></a>

### func RecordRoute

```go
func RecordRoute(router http.Handler) http.Handler
```

RecordRoute wraps the application's router, normally the http.ServeMux at the end of the middleware chain, so [Telemetry.HTTPMiddleware](#Telemetry.HTTPMiddleware) names the span and labels HTTP metrics with the route pattern the router matched, even when middleware between them replaced the request with r.WithContext:

```
handler := httpx.Chain(telemetry.RecordRoute(mux), tel.HTTPMiddleware(), auth, …)
```

Routes are the patterns registered in code, so they keep metric series bounded whatever paths clients request. Without HTTPMiddleware, RecordRoute does nothing.

*Since `v0.1.0`*

## Types

<a id="LogFormat"></a>

### type LogFormat

```go
type LogFormat string
```

LogFormat selects the log encoding.

*Since `v0.1.0`*

<a id="LogFormatJSON"></a>
<a id="LogFormatText"></a>

```go
const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)
```

Log formats.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [Setup](#Setup).

*Since `v0.1.0`*

<a id="WithLogFormat"></a>

#### func WithLogFormat

```go
func WithLogFormat(f LogFormat) Option
```

WithLogFormat sets JSON (default, for production) or text (for local development) logs.

*Since `v0.1.0`*

<a id="WithLogLevel"></a>

#### func WithLogLevel

```go
func WithLogLevel(l slog.Leveler) Option
```

WithLogLevel sets the minimum log level. Default: info.

*Since `v0.1.0`*

<a id="WithLogTee"></a>

#### func WithLogTee

```go
func WithLogTee(h slog.Handler) Option
```

WithLogTee also sends every log record to h, such as the development console's buffer of recent records (ADR-0065) or the hourly log archive (ADR-0079). h's Enabled decides which records it receives, independently of [WithLogLevel](#WithLogLevel), and it sees the request, trace and organisation attributes the logger adds. Given more than once, every handler receives every record. A nil h adds nothing. Default: none.

*Since `v0.1.0`*

<a id="WithLogWriter"></a>

#### func WithLogWriter

```go
func WithLogWriter(w io.Writer) Option
```

WithLogWriter sets where logs are written. Default: os.Stdout.

*Since `v0.1.0`*

<a id="WithOTLPExport"></a>

#### func WithOTLPExport

```go
func WithOTLPExport(enabled bool) Option
```

WithOTLPExport enables exporting spans and metrics over OTLP/HTTP, configured by the standard OTEL\_EXPORTER\_OTLP\_\* environment variables.

*Since `v0.1.0`*

<a id="WithPrometheus"></a>

#### func WithPrometheus

```go
func WithPrometheus(enabled bool) Option
```

WithPrometheus enables a Prometheus exporter, alongside OTLP export when both are on: [Telemetry.MetricsHandler](#Telemetry.MetricsHandler) then serves every metric of the meter provider in the Prometheus text format (ADR-0063). The exporter uses its own registry, never Prometheus's global one. Default: off.

*Since `v0.1.0`*

<a id="WithRuntimeMetrics"></a>

#### func WithRuntimeMetrics

```go
func WithRuntimeMetrics() Option
```

WithRuntimeMetrics records Go runtime metrics (memory, garbage collector goal, goroutines, GOMAXPROCS) with the OpenTelemetry runtime instrumentation. They are read from runtime/metrics when metrics are collected, so they cost nothing while no exporter is on. Default: off.

*Since `v0.1.0`*

<a id="WithSampleRatio"></a>

#### func WithSampleRatio

```go
func WithSampleRatio(r float64) Option
```

WithSampleRatio sets the fraction of new traces sampled, from 0 to 1. Child spans follow their parent's decision. Default: 1.

*Since `v0.1.0`*

<a id="WithSpanExporter"></a>

#### func WithSpanExporter

```go
func WithSpanExporter(e sdktrace.SpanExporter) Option
```

WithSpanExporter adds a synchronous span exporter, for tests and custom backends.

*Since `v0.1.0`*

<a id="WithTraceContextFrom"></a>

#### func WithTraceContextFrom

```go
func WithTraceContextFrom(callers []netip.Prefix) Option
```

WithTraceContextFrom makes [Telemetry.HTTPMiddleware](#Telemetry.HTTPMiddleware) continue the trace, and accept the baggage, of requests whose client address is in callers: gateways and internal services that start traces. Match addresses as the middleware sees them, after httpx.TrustedProxies. Requests from other addresses start a new trace linked to the incoming one. Default: none.

*Since `v0.1.0`*

<a id="WithoutGlobals"></a>

#### func WithoutGlobals

```go
func WithoutGlobals() Option
```

WithoutGlobals keeps Setup from installing OpenTelemetry globals.

*Since `v0.1.0`*

<a id="Telemetry"></a>

### type Telemetry

```go
type Telemetry struct {
	// contains filtered or unexported fields
}
```

Telemetry holds the configured logger and OpenTelemetry providers.

*Since `v0.1.0`*

<a id="Setup"></a>

#### func Setup

```go
func Setup(ctx context.Context, service, version string, opts ...Option) (*Telemetry, error)
```

Setup configures telemetry for service at version.

*Since `v0.1.0`*

<a id="Telemetry.HTTPMiddleware"></a>

#### func (*Telemetry) HTTPMiddleware

```go
func (t *Telemetry) HTTPMiddleware() func(http.Handler) http.Handler
```

HTTPMiddleware starts a server span for each request, records standard HTTP server metrics, and names the span after the matched http.ServeMux pattern (for example "GET /v1/projects/{id}") so traces group by route, not by raw path.

Incoming W3C trace context is untrusted by default: each request starts a new trace, sampled by [WithSampleRatio](#WithSampleRatio), that links to the caller's span, and incoming baggage is ignored. A client can't hide its requests from tracing, force sampling, or join another request's trace. Requests from the callers given to [WithTraceContextFrom](#WithTraceContextFrom) continue their trace instead.

The span's client.address is the request's RemoteAddr, as resolved by httpx.TrustedProxies when installed before this middleware, never a raw X-Forwarded-For header. Metrics leave out server.address and server.port, which come from the client's Host header, and carry http.route, the matched pattern without its method, instead of the path.

http.ServeMux sets the pattern on the request it routes, which is a copy when a middleware between this one and the mux calls r.WithContext. Wrap the mux with [RecordRoute](#RecordRoute) so the route reaches spans and metrics anyway.

*Since `v0.1.0`*

<a id="Telemetry.Logger"></a>

#### func (*Telemetry) Logger

```go
func (t *Telemetry) Logger() *slog.Logger
```

Logger returns the correlated structured logger.

*Since `v0.1.0`*

<a id="Telemetry.MeterProvider"></a>

#### func (*Telemetry) MeterProvider

```go
func (t *Telemetry) MeterProvider() metric.MeterProvider
```

MeterProvider returns the meter provider.

*Since `v0.1.0`*

<a id="Telemetry.MetricsHandler"></a>

#### func (*Telemetry) MetricsHandler

```go
func (t *Telemetry) MetricsHandler() http.Handler
```

MetricsHandler returns the Prometheus scrape handler enabled by [WithPrometheus](#WithPrometheus), or a handler answering 404 when it's off. Serve it on a separate listener bound to a private interface, never on the API's public address: it has no authentication, and metrics describe the service's routes and load.

Scrapes collect on demand. At most four run at once (others get 503), each for up to 10 seconds; collection errors are logged and the metrics that could be collected are still served.

*Since `v0.1.0`*

<a id="Telemetry.Shutdown"></a>

#### func (*Telemetry) Shutdown

```go
func (t *Telemetry) Shutdown(ctx context.Context) error
```

Shutdown flushes and stops exporters. Register it first on the cleanup stack so it runs last.

*Since `v0.1.0`*

<a id="Telemetry.TracerProvider"></a>

#### func (*Telemetry) TracerProvider

```go
func (t *Telemetry) TracerProvider() trace.TracerProvider
```

TracerProvider returns the tracer provider.

*Since `v0.1.0`*
