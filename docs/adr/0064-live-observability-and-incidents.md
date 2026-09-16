# ADR-0064: Live observability and incident reports

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0026, ADR-0051

## Context

v1.1 adds live observability and incidents (roadmap, feature 9): request, error and latency windows shared by every instance through PostgreSQL, `/ops/observability` with a live stream, and incidents opened by operators or by error-rate thresholds, with updates, resolution, reports and audit events. APIs only; dashboards stay on mock data. Today:

| Area | Today | Evidence |
|---|---|---|
| HTTP telemetry | otelhttp metrics and spans per route (`telemetry.RecordRoute`), exported over OTLP or Prometheus to an external backend. Nothing inside the app can answer "what is the error rate right now, on every instance" | ADR-0007, ADR-0063 |
| Instance view | `GET /ops/system` describes one instance; ADR-0051 calls a cluster view v1.1's job. ADR-0026's v1.1 line planned "per-instance in-memory recent logs and metrics over SSE" and lists "not aggregated across instances" as a trade-off | ADR-0026, ADR-0051 |
| Route patterns | The auth middleware copies the request, so `r.Pattern` is only visible through a recorder around the mux (ADR-0063). Access logs still log the raw path as `route` in Full apps | `modules/telemetry/http.go`, `httpx.AccessLog` |
| Instances | Each process has a random instance ID and heartbeats in `release_instances` | ADR-0040, `releases.Tracker.InstanceID` |
| Jobs | Periodic River jobs are enqueued by the elected leader, once per deployment; run now is limited to one queued run | ADR-0033 |
| Audit | Events record actor, resource, outcome, request ID, IP address, user agent and metadata; `auditpg.Store.List` filters by time | ADR-0036, ADR-0051 |
| Ops authorisation | Every use case calls `authorize` (platform role with 2FA); the reflection test fails an operation that doesn't | ADR-0051 |
| Long responses | `httpx.Server` has a 60-second write timeout and a 20-second shutdown; nothing streams today | `httpx/server.go` |

Constraints: never a raw path, query string or other client-chosen value as a series key (HTTP-2, ADR-0053); bounded memory per instance; the database write per instance must be cheap and measured; application logs stay out of PostgreSQL (ADR-0026 "not built"); modules never import each other (ADR-0019).

## Options

### Where request windows live

| Option | Verdict |
|---|---|
| In memory per instance only, streamed over SSE (ADR-0026's original plan) | Rejected: behind a load balancer each request reaches one instance, so the view is a sample, and a restart loses it. The roadmap asks for windows shared across instances |
| Read the Prometheus or OTLP data back | Rejected: needs a backend the app doesn't run, and `METRICS_ADDR` is off by default |
| **Per-instance minute windows written to PostgreSQL every 15 seconds, one row per instance, minute, method and route** | **Chosen**: every instance sees the deployment; one small upsert per instance; queries aggregate rows |
| One row per request | Rejected: turns the database into a request log (ADR-0026) |

### Latency

| Option | Verdict |
|---|---|
| Exact percentiles (keep durations) | Rejected: memory grows with traffic, and percentiles can't be added across instances |
| t-digest or DDSketch | Rejected: mergeable and accurate, but a serialized sketch per row is opaque to SQL and needs a dependency or a custom encoder |
| **Fixed histogram buckets (35), summed element by element in SQL** | **Chosen**: mergeable across instances and minutes by addition, fixed size, readable in SQL; the estimation error is bounded by bucket width and documented |

### Library or app code

| Option | Verdict |
|---|---|
| Collector in `modules/telemetry` | Rejected: the store needs pgx, which telemetry doesn't depend on; and a telemetry option would couple an OpenTelemetry module to an app database table |
| Everything app-owned | Rejected: the collector, the aggregation query and the one-open-incident rule behave the same in every app and should be fixed with `go get` (ADR-0040's argument) |
| **New module `modules/observability` (collector, middleware, store, incidents, detection, stream limits); the app's `ops` module composes the endpoints and the report from it, `auditpg` and `releases`** | **Chosen** |
| A separate `modules/incidents` | Rejected: detection needs the request minutes and the incidents in one transaction; two modules couldn't share it without importing each other |

### Route capture

| Option | Verdict |
|---|---|
| Read the route from `telemetry.RecordRoute`'s recorder | Rejected: modules can't import each other, and the recorder is private |
| Place the collector just around the mux | Rejected: misses time and responses of middleware (authentication, rate limits, maintenance mode) |
| **The collector's middleware early in the chain, plus `observability.RecordRoute(mux)` like ADR-0063** | **Chosen**: counts every response, the route comes back through a recorder in the context |

### De-duplicating automatic incidents

| Option | Verdict |
|---|---|
| Rely on River's leader enqueuing periodic jobs once | Rejected as the only guard: run now, a leader change or a retry can overlap runs |
| **Transaction advisory lock around detection, and a partial unique index allowing one open automatic incident** | **Chosen**: serialised decisions, and the database refuses a second incident even if the lock is bypassed |

### Live stream

| Option | Verdict |
|---|---|
| WebSockets | Rejected: needs a dependency and an upgrade path through proxies; the stream is one-way |
| Clients poll the overview | Kept possible; the stream saves clients from writing the loop |
| **Server-Sent Events: an overview every 5 seconds, at most 10 minutes, 2 per user and 20 per instance, the session checked before every event** | **Chosen** |

## Decision

### `modules/observability`

| Piece | Decision |
|---|---|
| `Collector` | Per instance, one window per minute: a map from (method, route) to counters (requests, 4xx, 5xx, duration sum, max) and a 35-bucket histogram, updated with atomics under a read lock. At most 500 series a minute (`WithMaxSeries`; more count as `_OTHER _overflow`); at most 10 finished minutes wait for a failing database (`WithMaxPendingMinutes`, the oldest dropped and counted in `Lost`). Non-standard methods become `_OTHER` |
| `Collector.Middleware`, `RecordRoute` | Counts every request after `httpx.Recover`; a panicking handler counts as 500; the route is the pattern's path (`/v1/projects/{id}`), `""` when none matched; paths, queries, headers are never keys |
| `Collector.Run` | A runner (ADR-0017) writing every 15 seconds (`WithFlushInterval`, 1 s–1 min), and once more when stopped, so a clean shutdown loses nothing. Minutes are absolute totals: a minute is written again while it runs and upserted; a write with fewer requests than stored changes nothing. A finished minute is forgotten after its last write |
| `Collector.Subscribe` | For the dev console (feature 10): subscribers get every request, with its path, request ID and trace ID, on the request goroutine; with no subscriber the path isn't read |
| `observability_minutes` | Primary key (minute, instance_id, method, route); `buckets bigint[]`; migration `00001` |
| `Store.Summary(from, to)` | One `GROUP BY GROUPING SETS ((), (minute), (instance_id), (method, route))` statement, buckets summed with one `sum(buckets[i])` per bucket; 5-second timeout (`ErrQueryTimeout`); at most 7 days |
| `Stats.Quantile` | Linear interpolation inside the bucket holding the rank; the last bucket up to the maximum; never above the maximum |
| `Store.DeleteBefore`, `Oldest` | Batched retention |
| Incidents | `incidents` and `incident_updates` (migration `00002`): title (200), summary (5,000), severity `sev1`–`sev4`, status `investigating`, `identified`, `monitoring`, `resolved`, source `manual` or `automatic`, `started_at` (up to 90 days back), `resolved_at`, `recovered_at`, creator; updates with kind, message (5,000), status and severity after it, actor. At most 500 updates. `OpenIncident`, `UpdateIncident`, `ResolveIncident` lock the row; resolved incidents refuse changes |
| `Store.DetectIncident` | In one transaction: advisory lock, sum of requests and 5xx over the window up to the current minute, the open automatic incident `FOR UPDATE`. Above the threshold with at least `MinRequests`: open one (sev2) or, if it had recovered, add a `breaching` update. At or below with at least `MinRequests`, not yet recovered: set `recovered_at` and add a `recovered` update. Never resolves. Counter `incidents.detections{action}` |
| `Streams` | Limits long-lived responses: total, per subject, max duration (context cause `ErrStreamExpired`), `Close` for shutdown (`ErrStreamsClosed`) |

### Apps (both Full apps)

| Piece | Decision |
|---|---|
| Wiring | `internal/app/observability.go`; the collector uses the release tracker's instance ID, runs with the workers; middleware after tracing; `telemetry.RecordRoute(observability.RecordRoute(mux))`; `OnShutdown(streams.Close)` |
| Endpoints | `GET /ops/observability/overview`, `/routes`, `/stream` (`ops.observability.read`); `POST /ops/incidents`, `GET /ops/incidents`, `GET /ops/incidents/{id}`, `POST /ops/incidents/{id}/updates`, `POST /ops/incidents/{id}/resolve` (`ops.incidents.write` for changes), `GET /ops/incidents/{id}/report` (`ops.incidents.read`) |
| Stream | `text/event-stream`, `retry: 5000`, `overview` events, a final `end` event with the reason; `X-Accel-Buffering: no`, `Cache-Control: no-store`; each write gets a 10-second deadline instead of the server's write timeout. The permission is checked on connect (errors are problem+json before any event); before every later event the session token is authenticated again and the permission checked, so a signed-out, expired or revoked session, or a removed role, ends it within 5 seconds |
| Report | Window: 15 minutes before the start to 15 minutes after resolution (or now), at most 24 hours. Sections `requests` (`ops.observability.read`), `audit_events` (`ops.audit.read`; at most 200; only ID, time, action, outcome, actor kind and ID, resource, request ID) and `releases` (`ops.releases.read`; instance starts grouped by build); a section the caller can't read is left out with a note. JSON, or Markdown with `format=markdown` or `Accept: text/markdown`, escaping stored text |
| Audit | `ops.incident.opened`, `ops.incident.updated`, `ops.incident.resolved`, with source, status, severity and update ID, never titles or messages; the detection job records them as `system:incidents_detect` |
| Settings | `observability.retention` (24 h, 1 h–7 d), `incidents.detection_window` (5 min, 1 min–1 h), `incidents.error_rate_threshold` (5 %, 0.1–100), `incidents.min_requests` (100, 1–1,000,000); all need a reason |
| Jobs | `observability_cleanup` (hourly, batches of 5,000), `incidents_detect` (every minute, 30 s timeout, 1 attempt); `observability_minutes` in `GET /ops/retention` |
| Permissions | `ops.observability.read`, `ops.incidents.read` (`ops_viewer`, `platform_admin`), `ops.incidents.write` (`platform_admin`); 2FA like all ops roles |
| Error codes | `invalid_observability_window` (422), `observability_query_timeout` (503), `observability_streams_limited` (429), `incident_not_found` (404), `invalid_incident` (422), `incident_resolved` (409), `incident_updates_limited` (409) |

## Why

- Minute rows per instance make the cluster view a `GROUP BY` and survive restarts, at the cost of one upsert per instance every 15 seconds.
- Histograms add up across instances and minutes; a bounded, documented estimation error is enough to see "p95 doubled".
- Keys from code patterns keep series bounded whatever clients send, the rule ADR-0053 set for metrics.
- The lock and the unique index make "one automatic incident" a database fact, not a scheduling assumption.
- Rechecking the session before each event keeps a long-lived response from outliving the access that opened it.
- A report that omits sections by permission lets `ops.incidents.read` be granted without widening audit access.

## Trade-offs

- The overview lags by up to 15 seconds per instance, and a crashed instance loses its last 15 seconds.
- Percentiles are estimates (measurements below). Latency is measured from the collector's middleware, so time in `Recover`, trusted proxies, request IDs and tracing before it isn't counted.
- Requests answered by middleware before routing (maintenance, rate limits, body limits, authentication outages) have the route `""`: visible in totals and error rates, not per route.
- The error rate counts every 5xx, including maintenance mode's 503: turning maintenance on for more than `incidents.detection_window` with traffic opens an automatic incident. Operators disable `incidents_detect` during planned maintenance or resolve it.
- No traffic never counts as recovered: an app that stops receiving requests keeps its incident open until an operator resolves it.
- Stream checks cost one session lookup per stream every 5 seconds (at most 20 per instance).
- Incidents are kept forever; titles, summaries and messages are free text operators may fill with personal data. The docs ask them not to.
- No alert delivery (paging, chat): a log line at warning level and the `incidents.detections` counter are what external alerting can use.

## Consequences

- ADR-0026: its v1.1 "live observability" becomes shared minute windows, not per-instance in-memory data, and its trade-off "not aggregated across instances" no longer applies; "incident reports" are incidents with timelines, without grouped errors.
- ADR-0051: `GET /ops/system` stays per instance; the cluster view is `/ops/observability`. The retention summary gains `observability_minutes`.
- New public names (surface.json): the endpoints, permissions, settings, jobs, error codes and audit actions above; the `observability_minutes` bucket bounds are stored data.
- Threat model: a new row for request data exposure, stream abuse and forged or flooding incidents.
- `modules/observability` joins the CI lists; `api/modules-observability.txt` records its API.

## Implementation notes (2026-09-16)

- **Bucket bounds:** 0.25, 0.5, 1, 1.5, 2, 3, 4, 5, 7.5, 10, 15, 20, 30, 40, 50, 75, 100, 150, 200, 300, 400, 500, 750 ms, 1, 1.5, 2, 3, 4, 5, 7.5, 10, 15, 30, 60 s and a last bucket. Between 1 ms and 10 s no bound is more than 1.5 times the previous one, so a percentile estimate is within 50 % of the exact value there (within 0.5 ms below 1 ms, within 100 % above 10 s). Measured worst errors of p50, p90, p95 and p99 (`TestQuantileError`, 10,000 samples): lognormal with a 20 ms median 5.9 %, uniform 1–300 ms 0.6 %, bimodal 0.3 ms/80 ms 15.4 %, uniform 5–40 s 9.9 %.
- **Memory:** a series is about 420 bytes (35 atomic buckets, 5 counters, map entry). The bound is 501 series × 11 minutes ≈ 2.3 MB per instance; 50 routes over the current and previous minute take about 42 KB.
- **Performance** (Apple M1 Max, PostgreSQL 18 in Docker, `go test -bench`):

| Measurement | Result |
|---|---|
| Middleware and `RecordRoute`, through a request copy, handler doing nothing | +326 ns, +3 allocations, +416 B per request (592 ns against 266 ns without); 436 ns per request with 10 goroutines |
| One flush (`WriteMinutes`) | 10 series 0.65 ms, 100 series 2.0 ms, 500 series 7.0 ms; one statement every 15 seconds per instance |
| `Summary`, 3 instances × 50 routes | 15 minutes 5.8 ms, 1 hour 19 ms, 24 hours (216,000 rows) 635 ms. A first version summing buckets through `unnest` took 181 ms for 15 minutes and over 5 seconds for 24 hours |

- **Stream write deadline:** otelhttp wraps the writer with httpsnoop, whose `Unwrap` reaches `net/http`'s writer, so `http.ResponseController.SetWriteDeadline` works through the whole chain. The token for rechecks is read with `auth.TokenFrom` (bearer header, else the session cookie), as the auth middleware reads it.
- **Deviation from the roadmap line:** the overview is `GET /ops/observability/overview` as the feature notes name it; errors are 5xx only (4xx are reported separately and don't open incidents).

| Check | Result |
|---|---|
| `observability` `TestQuantileError`, `TestBucketOf`, `TestStatsWithoutRequests` | Estimates within the bucket width for four distributions; bound spacing checked |
| `TestCollectorCounts`, `TestCollectorSeriesAreBounded`, `TestCollectorFlushesMinutes`, `TestCollectorKeepsBoundedMinutesWhileTheSinkFails`, `TestCollectorLateRequests`, `TestCollectorRunWritesOnStop`, `TestCollectorConcurrentRecords` (race), `TestSubscribe` | Counting, overflow series, minute rotation and forgetting, pending minutes capped with `Lost`, final write on stop, 8 goroutines × 1,000 requests exact |
| `TestMiddlewareRecordsRoutes` | 150 requests with distinct paths, queries, hosts, forwarded addresses and methods through a request copy: 5 series, none holding a client value; panicking handler counted as 500; flushing works through the recorder |
| `TestSummaryAddsUpInstances` (PostgreSQL) | Three collectors as three instances over three minutes, 15 flushes: totals, per minute, per instance (last minute, last write), per route, merged histogram, max and p95 match what was served; an older snapshot doesn't replace a newer one; invalid ranges refused |
| `TestDetectIncidentLifecycle` (PostgreSQL) | Under min requests nothing; above threshold across two instances opens one sev2 incident; repeated runs change nothing; recovery adds one `recovered` update; no traffic changes nothing; high again adds `breaching`; after resolving a new incident opens |
| `TestDetectIncidentOncePerDeployment` | 30 concurrent detections from 3 pools open exactly 1 incident |
| `TestIncidentLifecycle`, `TestIncidentValidation`, `TestIncidentUpdatesAreBounded`, `TestListIncidents`, `TestStreams` | Lifecycle and actors, validation, 500 concurrent updates then `ErrTooManyUpdates`, filters and cursors, stream limits and causes |
| Apps `TestObservabilityOverview` | Scripted load (40 pings, 12 unmatched paths with secrets, 10 maintenance 503s): per-route and per-instance numbers, error rate, rate per minute, percentile order, minutes add up, no path or query in the response; routes sorted by errors; invalid windows 422 |
| Apps `TestObservabilityAcrossInstances` | Two app instances on one database: either overview shows both and their sum |
| Apps `TestObservabilityStream` | Real server: 401, 403, 422 on connect; headers; `retry` and a flushed `overview` event; third stream 429; signing out ends the stream with `end` `unauthorized` within one interval |
| Apps `TestIncidentsThroughOps` | 403 for viewers' writes, 422, 201, updates, filters, resolve, 409 after, 404; three audit events without titles; JSON report with requests, one release and audit events without IP, user agent or metadata; Markdown by `Accept` and by `format` with the title escaped |
| Apps `TestIncidentDetectionThroughJob` | A failing minute and run now: one automatic incident by `incidents_detect`, audited as `system` |
| Ops `TestObservabilityStreamEnds`, `TestIncidentReportSections` | Stream ends on max duration, session end, permission loss, shutdown, client gone and read failure; report sections left out per permission and noted when unreadable |
| `TestEveryOperationAuthorizesFirst`, `TestOpsOperationsDeclareSecurity`, `TestOpsAPICompatible`, `TestPublicSurface` | Pass: new operations authorise first and declare 401/403; `/ops` baseline unchanged (additions only); surface additions recorded |
