# ADR-0063: Prometheus metrics endpoint

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0007

## Context

v1.1 adds a Prometheus `/metrics` option, served on a separate `METRICS_ADDR` listener and off by default (roadmap, feature 8). Today:

| Area | Today | Evidence |
|---|---|---|
| Metrics export | OTLP/HTTP only, when `OTEL_EXPORTER_OTLP_ENDPOINT` is set: a periodic reader on the SDK meter provider. Without it, metrics are recorded and dropped | `modules/telemetry/telemetry.go` |
| What's recorded | otelhttp server metrics (`http.server.request.duration`, request and response body sizes); `ratelimit.decisions` and `ratelimit.fallbacks` (ADR-0052). No Go runtime metrics, no connection pool metrics | `modules/telemetry/http.go`, `modules/ratelimitpg` |
| Cardinality | HTTP-2 (security review): otelhttp took `server.address` and `server.port` from the client's `Host` header. `Setup` drops both with a view | ADR-0007 security review fixes, `TestHTTPMetricsIgnoreHost` |
| Route label | otelhttp adds `http.route` from `r.Pattern` of the request it passed on. In both Full apps the session middleware calls `r.WithContext`, so `http.ServeMux` sets the pattern on a copy: **no Full-app HTTP metric had a route, and every server span was named `GET`, `POST`…** (found while writing this feature's app test) | `modules/auth/middleware.go`, `routes.go` |
| Listeners | One `httpx.Server` per app (`APP_ADDR`) run by `lifecycle.Run` (ADR-0017); `/livez`, `/readyz`, `/version` are public on it | `internal/app/app.go` |
| Pool | `postgres.Open` traces queries (`WithTracerProvider`); pgx exposes `pool.Stat()` but nothing reads it | `modules/postgres/postgres.go` |
| Local development | `orb dev --observability` points OTLP at Grafana (`grafana/otel-lgtm`) | ADR-0028 |

Constraints: core may not depend on the OpenTelemetry SDK or exporters; `modules/telemetry` already holds them (ADR-0019). Infrastructure is configured by environment variables read in `config.go` (ADR-0031). A value a client controls must never become a metric attribute (HTTP-2).

## Options

### Exporter

| Option | Verdict |
|---|---|
| Hand-written Prometheus text encoder over a manual reader | Rejected: re-implements name translation, histogram buckets, escaping and content negotiation that the official exporter maintains and tests |
| Prometheus `client_golang` instruments instead of OpenTelemetry | Rejected: a second instrumentation API; modules instrument through OpenTelemetry (ADR-0007) |
| OpenTelemetry Collector sidecar scraping OTLP | Rejected as the only path: another process to run; still available to anyone who wants it |
| **OpenTelemetry Prometheus exporter (`go.opentelemetry.io/otel/exporters/prometheus`) as a second reader on the same meter provider, with its own registry** | **Chosen**: the same metrics over OTLP and Prometheus, maintained upstream, no global registry |

### Where it's served

| Option | Verdict |
|---|---|
| `/metrics` on the API listener | Rejected: public by default, and the API's middleware (maintenance, rate limits, CORS) would apply to scrapes |
| `/metrics` on the API listener behind a bearer token | Rejected: a secret to rotate in every scraper, and one leaked or brute-forced token exposes it to the internet |
| **A second `httpx.Server` on `METRICS_ADDR`, serving only `GET /metrics`, a `Runner` in the app's lifecycle** | **Chosen**: network-level isolation, the Prometheus norm; off unless configured |
| Optional `METRICS_TOKEN` on that listener | Deferred: the listener belongs on a private network, where a token adds rotation work for little gain. Additive later if platforms without private networking ask for it |

### Runtime and pool metrics

| Option | Verdict |
|---|---|
| Prometheus Go and process collectors | Rejected: Prometheus-only, so OTLP users wouldn't get them |
| **OpenTelemetry runtime instrumentation (`go.opentelemetry.io/contrib/instrumentation/runtime`), opt-in with `telemetry.WithRuntimeMetrics()`** | **Chosen**: semantic-convention `go.*` metrics on both exporters; read at collection, so free while nothing collects |
| **Pool statistics as asynchronous instruments in `modules/postgres`, opt-in with `postgres.WithMeterProvider(mp)`** | **Chosen**: one callback reading `pool.Stat()`; `otel/metric` is already in the module's graph |
| Pool metrics on the global meter provider by default, like tracing | Rejected: callbacks can't be unregistered when a pool closes (pgx has no close hook), so tests opening many pools would accumulate them |

### Route label

| Option | Verdict |
|---|---|
| Move the telemetry middleware after authentication | Rejected: spans would miss authentication, and the request copy still hides the pattern from anything outside it |
| Let the telemetry middleware resolve the route itself (`mux.Handler(r)`) | Rejected: it would need the mux and would match every request twice |
| **`telemetry.RecordRoute(mux)` around the router passes the matched pattern back through a recorder in the request context** | **Chosen**: one wrapper in `routes.go`; the route pattern goes on the span name, the span's `http.route` and the metrics' `http.route` (via otelhttp's labeler) whatever middleware copies the request |

## Decision

### `modules/telemetry`

```go
func WithPrometheus(enabled bool) Option // second reader, private registry
func WithRuntimeMetrics() Option          // go.* runtime metrics
func (t *Telemetry) MetricsHandler() http.Handler // 404 handler when off
func RecordRoute(router http.Handler) http.Handler
```

- The scrape handler (`promhttp.HandlerFor`) continues on collection errors and logs them, allows 4 concurrent scrapes (others get 503) and cuts one off after 10 seconds.
- `HTTPMiddleware` passes a route recorder in the request context. `RecordRoute`, or the middleware itself when the pattern is visible, records the first non-empty pattern: span name and `http.route` as today (`GET /v1/projects/{id}`), and `http.route` on metrics as otelhttp formats it (`/v1/projects/{id}`).
- Metric attributes stay what otelhttp and the HTTP-2 view leave: method (unknown methods become `_OTHER`), status code, scheme, protocol, route pattern. Paths, `Host`, query strings, user agents, forwarded addresses and trace headers never become attributes.

### `modules/postgres`

`WithMeterProvider(mp)`: `db.client.connection.count` (`db.client.connection.state` = `idle` or `used`), `db.client.connection.max`, `pgxpool.acquires`, `pgxpool.acquire.waits`, `pgxpool.acquire.wait_time`, `pgxpool.acquire.canceled`, `pgxpool.connections.created`, each labelled only with `db.client.connection.pool.name` (the application name).

### Apps (all three presets)

| Piece | Decision |
|---|---|
| `METRICS_ADDR` | Empty: off. Otherwise `host:port` with a numeric port; the same port as `APP_ADDR` (any host, so `0.0.0.0:8080` next to `127.0.0.1:8080` too) is refused at start. Port 0 is allowed for tests |
| `internal/app/metrics.go` | `newMetricsServer`: an `httpx.Server` with the API server's default timeouts and an error logger, a mux with only `GET /metrics`, no middleware and no authentication; `(*App).MetricsServer()` for tests; `checkMetricsAddr` |
| `app.go` | `WithPrometheus(cfg.MetricsAddr != "")`, `WithRuntimeMetrics()`, `postgres.WithMeterProvider` (Full); `Run` adds the metrics server to the runners, so it starts, fails and shuts down with the API; the start log line has `metrics_addr` |
| `routes.go` | `httpx.Chain(telemetry.RecordRoute(mux), …)` |
| Docs | Binding to a private interface or an unpublished container port, per-instance scraping, the metric list and example queries in the production guide |

## Why

- One set of instruments feeds both exporters, so switching between OTLP and Prometheus changes nothing in dashboards' metric meaning.
- A separate listener keeps metrics off the internet by default and out of the API's middleware, rate limits and maintenance mode.
- Refusing `APP_ADDR`'s port makes "metrics on the public address" a start-up error rather than a deployment review item.
- The route fix makes HTTP metrics per route, which is what operators alert on, while keeping the series bounded by the routes in code.

## Trade-offs

- Every app compiles in `client_golang`, `client_model`, `common`, `procfs`, `otlptranslator` and the exporter even when the listener is off: +25 packages in `full-single`'s build, +42 KB (Full) and +351 KB (Minimal) stripped binaries.
- No authentication on the listener: its safety depends on network configuration, which the docs spell out but the app can't check.
- Scrapes are large: default labels include `otel_scope_name`, `otel_scope_version` and `otel_scope_schema_url`, and each route and status has three histograms. 40 routes × 5 statuses give a 3.7 MB uncompressed exposition collected in about 13 ms (gzip when the scraper asks, as Prometheus does).
- The route recorder adds a request copy: 4 allocations and about 600 bytes per request. With the listener on, the second reader adds about 2.4 µs per request to HTTP metric recording (measured below).
- Apps that don't wrap their router with `RecordRoute` keep route-less metrics and spans named after the method when middleware copies the request.

## Consequences

- `modules/telemetry` gains `WithPrometheus`, `WithRuntimeMetrics`, `MetricsHandler` and `RecordRoute`; `modules/postgres` gains `WithMeterProvider`. Additions only.
- Apps gain `METRICS_ADDR`, `internal/app/metrics.go`, three lines in `app.go`, one in `config.go` and the `RecordRoute` wrapper in `routes.go`. Existing apps receive them through `orb upgrade`.
- With OTLP export on, apps now also export Go runtime and pool metrics and route-labelled HTTP metrics: more series at the backend.
- Threat model: the metrics listener is a new unauthenticated surface, mitigated by being off by default, refusing the API port, and documented private binding.
- ADR-0007's metric rules extend to Prometheus labels: no client-controlled values.

## Implementation notes (2026-09-16)

- **Versions** (all aligned with OpenTelemetry v1.46.0 / contrib v0.71.0 already in use): `go.opentelemetry.io/otel/exporters/prometheus` v0.68.0, `go.opentelemetry.io/contrib/instrumentation/runtime` v0.71.0, `github.com/prometheus/client_golang` v1.24.1, pulling `client_model` v0.6.2, `common` v0.70.1, `otlptranslator` v1.0.0, `procfs` v0.21.1 (v0.22.0 in Full apps, selected by goose), `beorn7/perks` v1.0.1, `munnerz/goautoneg`. No other module version changed. All are in the CI govulncheck run through `modules/telemetry` and the three examples.
- **Order in `Setup`:** the logger is built first (the scrape handler logs through it); the Prometheus reader is created before the OTLP exporters so a failure leaves nothing to shut down; runtime instruments are registered before globals are installed.
- **Pool name:** the application name, or `postgres` when none is set.
- **Access log:** `httpx.AccessLog` also reads `r.Pattern` and, in Full apps, logs the raw path as `route` for the same request-copy reason. Logs aren't metrics, so it isn't a cardinality problem; left for a follow-up in core.
- **Performance** (Apple M-series laptop, `go test -bench`, handler doing nothing): a request through `HTTPMiddleware` and `RecordRoute` takes 6.1 µs with 58 allocations (11.8 KB) without the Prometheus reader and 8.5 µs with 61 allocations (13.1 KB) with it; before the route recorder, 54 allocations (11.2 KB). A scrape of 200 route and status series plus runtime metrics takes 13 ms and 24 MB of allocations for 3.7 MB of text.

| Check | Result |
|---|---|
| `telemetry` `TestPrometheusScrape` | One `http_server_request_duration_seconds` series labelled `http_route="/v1/projects/{id}"` for three IDs; `go_goroutine_count`, `go_memory_used_bytes`, `go_processor_limit`, `target_info` with `service_name` |
| `telemetry` `TestPrometheusIgnoresClientValues` | 200 requests with distinct methods, paths, query strings, `Host`, `X-Forwarded-For`, user agents, `traceparent` and `baggage`: two series (route and unmatched), method `_OTHER`, none of the values in the scrape. Fails without the HTTP-2 view (`server_address="secret0.attacker.example"`) |
| `telemetry` `TestRecordRouteThroughRequestCopies` | With a middleware calling `r.WithContext` before the mux: span `GET /v1/projects/{id}` and `http_route` with `RecordRoute`; span `GET` and no `http_route` without it |
| `telemetry` `TestMetricsHandlerOffByDefault` | 404 without `WithPrometheus` |
| `postgres` `TestOpenReportsPoolMetrics` (Docker PostgreSQL) | With one connection held: `used` 1, an `idle` series, `max` 3, two acquires (ping and the held connection), the other counters present; every series has only the pool name and at most a state |
| Apps `TestMetricsListener` (all three) | The listener on `127.0.0.1:0` serves `http_route="/livez"`, runtime metrics and, in Full apps, `db_client_connection_count` and `pgxpool_acquires_total`; no client value from 20 `Host` headers and 20 unmatched paths; `/`, `/livez`, `/docs` and `/metrics/extra` are 404 on it; the API answers 404 to `/metrics`. Failed in Full apps before `RecordRoute` (no `http_route`) |
| Apps `TestMetricsListenerOffByDefault`, `TestLoadConfigMetricsAddr` | No listener without `METRICS_ADDR`; the API port with the same host, another host or an empty host, a missing port and a named port are refused; another port, and port 0 twice, are accepted |
