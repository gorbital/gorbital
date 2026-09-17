# Live observability and incidents

Full apps answer "how is the API doing right now, on every instance" without an external backend, and keep a record of incidents: what went wrong, when, what operators did, and what the requests, audit log and deploys show for that time. Decision: [ADR-0064](../adr/0064-live-observability-and-incidents.md). Library: `gorbital.dev/modules/observability`. Endpoints: [ops API](ops-api.md#observability).

This complements, and doesn't replace, logs, traces and metrics in your observability backend ([production](production.md#observe)): it keeps a day of per-minute counts, not requests.

## How requests are counted

Each instance runs a collector. Its middleware, early in the chain, counts every finished request in a one-minute window keyed by method and **route pattern**, the pattern registered in code such as `/v1/projects/{id}`:

| Counted | |
|---|---|
| `requests` | Every request, including those answered by middleware |
| `client_errors`, `server_errors` | 4xx and 5xx responses. A handler that panics counts as a 500 |
| Latency | Duration sum, maximum, and a histogram of 35 buckets from 0.25 ms to 60 s |

Every 15 seconds the collector writes its minutes to the `observability_minutes` table, one row per instance, minute, method and route, replacing the previous write of the same minute. A clean shutdown writes once more. Queries add the rows of every instance, so the overview lags by at most 15 seconds, and a crashed instance loses at most its last 15 seconds.

What is never kept: requested paths, query strings, headers, client addresses, user or organisation IDs. A request no route matched (`/v1/nope`) counts under the catch-all route `/`; requests answered before routing (maintenance mode, rate limits, oversized bodies, an authentication outage) have the route `""`. Methods other than the standard ones are `_OTHER`. So a client can't create series, and an instance holds at most 500 series a minute (more count as `_overflow`) for at most the current minute and 10 unwritten ones while the database is unreachable: about 2.3 MB at worst, tens of kilobytes normally.

The request counts are kept for `observability.retention` (24 hours; 1 hour to 7 days) and deleted hourly by the `observability_cleanup` job.

## Reading the overview

```bash
curl -H "Authorization: Bearer $TOKEN" 'http://127.0.0.1:8080/ops/observability/overview?window=15m'
```

`window` is whole minutes from `1m` to `24h` and ends with the current minute. Needs `ops.observability.read` (`ops_viewer`, `platform_admin`).

```json
{
  "window": "15m0s", "window_seconds": 900, "from": "2026-09-16T14:46:00Z", "to": "2026-09-16T15:01:00Z",
  "requests": 12040, "requests_per_minute": 802.7, "client_errors": 96, "server_errors": 12, "error_rate": 0.001,
  "latency_ms": {"mean": 18.4, "p50": 12.1, "p95": 61.7, "p99": 140.2, "max": 802.5},
  "instances": [{"instance_id": "9f3c2a7b41d8e605", "last_minute": "2026-09-16T15:00:00Z", "last_write": "2026-09-16T15:00:42Z", "requests": 6020, "…": "…"}],
  "top_routes": {"by_requests": [{"method": "GET", "route": "/v1/projects", "requests": 5210, "…": "…"}], "by_errors": [], "by_latency": []},
  "minutes": [{"minute": "2026-09-16T14:46:00Z", "requests": 790, "server_errors": 0, "p95_ms": 58.3}]
}
```

- `error_rate` is server errors per request, from 0 to 1.
- `instance_id` is the one in `GET /ops/releases/instances`. An instance whose `last_write` is more than 15 seconds old has stopped or can't reach the database.
- `GET /ops/observability/routes?window=1h&sort=p95&limit=20` lists every route, sorted by `requests`, `errors`, `error_rate`, `p95` or `p99`.

### How accurate the percentiles are

`p50`, `p95` and `p99` are estimated from the histogram: the estimate finds the bucket holding the percentile and interpolates inside it. The exact value is in the same bucket, so the error is at most the bucket's width:

| Exact latency | Error at most |
|---|---|
| Under 1 ms | 0.5 ms |
| 1 ms to 10 s | 50% of the value (bucket bounds grow by at most 1.5 times: 1, 1.5, 2, 3, 4, 5, 7.5, 10 ms, …) |
| Over 10 s | 100% (buckets at 15, 30 and 60 s, then the maximum) |

In practice latencies spread evenly enough within a bucket that errors are a few percent: measured on 10,000 samples, the worst of p50, p90, p95 and p99 was 5.9% for a lognormal distribution with a 20 ms median and 0.6% for uniform latencies from 1 to 300 ms; a bimodal mix of 0.3 ms and 80 ms requests was within 15.4%. `max` and `mean` are exact. The histograms of all instances and minutes add up exactly, so the error doesn't grow with the window or the number of instances.

Latency is measured from the collector's middleware to the end of the response; panic recovery, trusted proxies, request IDs and tracing, which run before it, aren't included.

## The local log store

In development, `orb dev` keeps the app's log records for the Dev Portal's Logs screen ([ADR-0072](../adr/0072-local-log-store.md)): every line the app writes (JSON, since `orb dev` sets `APP_LOG_FORMAT=json` unless `.env` chose; the terminal still shows text), `orb dev`'s own messages, and the PostgreSQL container's log when `orb dev` started the services. Records are JSON Lines under `.orb/portal/logs` (gitignored, mode 0600): segments of 8 MiB, 64 MiB in all, the oldest segment dropped first, so the store is bounded and survives the app's restarts. Each record has a `source` (`http` for the access log, `auth`, `jobs` and `mail` from the loggers the app hands those parts, `postgres`, `orb`, or `app`), and request records carry `method`, `path`, `route`, `status`, `duration_ms`, `request_id` and the signed-in `user_id`, so the screen filters by any of them. The Project Settings screen shows the store's size and clears it; `rm -r .orb/portal/logs` does the same.

The store is development only: production logs go wherever `APP_LOG_FORMAT=json` output is shipped.

## The hourly log archive

When you have no log pipeline, or want a durable copy anyway, the app can keep each hour's records in its own [file storage](storage.md) ([ADR-0079](../adr/0079-hourly-log-archive.md)). It is off until an operator turns on the runtime setting `logs.archive.enabled` (a reason is required; [settings reference](../reference/settings.md)):

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/logs.archive.enabled \
  -H 'Authorization: Bearer <token>' -H 'Content-Type: application/json' \
  -d '{"value":true,"version":0,"reason":"keep the logs during the migration"}'
```

From the next record on, every instance copies what it logs (at `APP_LOG_LEVEL` and above, as JSON lines with a `service` attribute, whatever `APP_LOG_FORMAT` says) into a file for the current hour under `LOG_ARCHIVE_DIR` (`.orb/logs` by default, created on demand; in the generated image that is `/home/nonroot/.orb/logs` inside the container, so point it at a mounted volume if a crashed instance's hour must survive the container). Within a minute of the top of the hour the finished file is gzipped, stored in the bucket and removed:

```text
logs/<service>/<YYYY>/<MM>/<DD>/<HH>.<host>.jsonl.gz                     a finished hour
logs/<service>/<YYYY>/<MM>/<DD>/<HH>.<host>.partial-<unix>.jsonl.gz      the rest of an hour, at shutdown or when switched off
```

Hours are UTC; `<host>` is the instance's host name, so instances sharing a bucket keep their own hours. The objects are `application/gzip` and show up like any other under `logs/` in `GET /ops/storage/objects?prefix=logs/` and the Dev Portal's Storage screen; `gunzip -c 10.web-1.jsonl.gz | jq` reads one. Turning the setting off stops collecting at once and stores what was collected so far as a partial hour; turning it on again in the same hour starts a new file, stored at the top of the hour as usual.

What can go wrong, and what happens:

- **The upload fails** (the bucket is unreachable, the credentials are wrong): the app logs `log archive upload failed; retrying at the next tick` with the key and the error, keeps the file, and tries again every minute. Nothing is lost while the disk holds.
- **The instance crashes**: the hour's file is on disk, appended to if the instance restarts within the hour and stored at the next start otherwise. The spool is buffered and flushed every minute, so a crash can lose the last minute's records from the archive; standard output still has them.
- **The directory can't be written**: the app logs `log archive can't write its spool file` and drops records from the archive (never from standard output) until it can.
- **The setting is on but the app has no file storage**: one warning, nothing collected.

The archive is a copy, not a search: the Logs screen reads `orb dev`'s store above, and querying production logs remains the job of a pipeline. Log records carry IDs, paths and addresses (never emails, tokens or secrets), so set a lifecycle rule on `logs/` in the bucket that matches your retention policy; the app never deletes what it stored.

## The Observability screen

The Dev Portal's Observability screen ([Dev Portal guide](dev-portal.md), [ADR-0073](../adr/0073-observability-screen.md)) shows the overview above for the running app, and what only the developer's machine can see:

- **Health**: the app's readiness, PostgreSQL (connections against `max_connections`, sessions waiting on locks), Mailpit, and every other Compose service's state.
- **Database**: `GET /_portal/api/db/stats` reads `pg_stat_database`, `pg_stat_activity`, `pg_locks` and the relation sizes: cache and index hit ratios, transactions and deadlocks, the largest tables with their scans and dead rows, lock waits with the blocking sessions, and statements running for over a second. The pool's counters come from `/ops/system`.
- **Queries**: `GET /_portal/api/db/statements` reads `pg_stat_statements`, sorted by total time, mean time, calls, rows or max time, with each statement's share of the total and its buffer hit ratio; Explain runs the SQL editor's `EXPLAIN` on the normalised text; Reset forgets the counters. The development `compose.yaml` preloads the extension (`command: ["postgres", "-c", "shared_preload_libraries=pg_stat_statements"]`); `orb dev` creates it on first use. Without the preload the view says what to add.
- **Advice**: `GET /_portal/api/db/advice` lists foreign keys without an index, indexes never scanned since the statistics reset, tables read mostly by sequential scans, and tables waiting for a vacuum, each with its numbers and the SQL to run in the SQL editor. They are suggestions: check them against real traffic before a migration.
- **System**: `GET /_portal/api/system` is `orb dev`'s sample (every 2 seconds, `gopsutil`) of the host's CPU, load, memory and the app directory's volume, and of the app process and `orb` themselves (CPU, resident memory, threads, open files); the Go runtime (goroutines, heap, GC) comes from `/ops/system`.

Traces stay in Grafana: `orb dev --observability` ([local development](local-development.md)).

## Streaming it

`GET /ops/observability/stream` sends the overview as [Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html) every 5 seconds:

```text
retry: 5000

event: overview
data: {"window":"15m0s","requests":12040,…}

event: overview
data: {…}

event: end
data: {"reason":"max_duration"}
```

| Rule | |
|---|---|
| Access | `ops.observability.read`, checked when the stream opens: 401, 403 or 422 as problem+json before any event |
| Session | Checked again before every event: signing out, an expired or revoked session, or a removed role ends the stream with `end` `unauthorized` within 5 seconds |
| Duration | 10 minutes, then `end` `max_duration`. Reconnect: browsers' `EventSource` does so after `retry` |
| Limits | 2 streams per user and 20 per instance; more get 429 `observability_streams_limited` |
| Shutdown | An instance shutting down ends its streams with `end` `shutting_down`, so it isn't held open |

`EventSource` in browsers sends the session cookie but no `Authorization` header; other clients send the bearer token:

```bash
curl -N -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/ops/observability/stream?window=5m
```

**Proxies and load balancers must not buffer the response**, or events arrive in bursts or only when the stream ends. The app sets `Cache-Control: no-store` and `X-Accel-Buffering: no` (which nginx honours); elsewhere turn buffering off for `/ops/observability/stream` (for example `proxy_buffering off;` in nginx, or response streaming on your platform), and allow responses of at least 10 minutes with 5-second gaps. Compression in front of the stream must flush each event.

## Incidents

An incident has a title, a summary, a severity (`sev1`, the most severe, to `sev4`), a status and a timeline of updates:

```text
investigating → identified → monitoring → resolved
```

Open incidents move between the first three in any order; resolved is final. Operators with `ops.incidents.write` (`platform_admin`) open, update and resolve them:

```bash
curl -X POST http://127.0.0.1:8080/ops/incidents -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout requests failing","severity":"sev1","summary":"Payments time out.","started_at":"2026-09-16T14:52:00Z"}'
curl -X POST http://127.0.0.1:8080/ops/incidents/12/updates -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"message":"The payment provider confirmed an outage.","status":"identified"}'
curl -X POST http://127.0.0.1:8080/ops/incidents/12/resolve -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"message":"The provider recovered; failed payments were retried."}'
```

Each change records an audit event (`ops.incident.opened`, `ops.incident.updated`, `ops.incident.resolved`) with the incident ID, status and severity, but not the text. Titles, summaries and messages are kept for as long as the incident, which is forever: write them for operators and don't include customers' names, addresses or other personal data.

### Automatic incidents

The `incidents_detect` job runs every minute. It adds up every instance's requests over `incidents.detection_window` (5 minutes, including the current one) and compares the share of server errors with `incidents.error_rate_threshold`:

| Situation | What happens |
|---|---|
| Above the threshold, with at least `incidents.min_requests` requests, and no automatic incident open | Opens a `sev2` automatic incident, `investigating`, started at the window's start |
| Still above the threshold | Nothing |
| At or below the threshold with enough requests, while one is open | Adds a `recovered` update and sets `recovered_at`, once. The incident stays open: an operator decides it is over and resolves it |
| Above again after recovering | Adds a `breaching` update |
| Fewer than `incidents.min_requests` requests | Nothing: a few failures of a quiet app don't open incidents, and no traffic never looks recovered |

At most one automatic incident is open at a time, whichever instance runs the job: detection holds a database lock, and a unique index refuses a second open one. Each change is logged (`incident opened: error rate above threshold` at warning level), counted in the `incidents.detections` metric (attribute `action`) and recorded as an audit event by `system:incidents_detect`. Nothing is paged or posted to chat: alert on the log line or the metric in your observability backend.

| Setting | Default | Bounds |
|---|---|---|
| `incidents.detection_window` | 5 minutes | 1 minute to 1 hour |
| `incidents.error_rate_threshold` | 5 (percent of requests) | 0.1 to 100 |
| `incidents.min_requests` | 100 | 1 to 1,000,000 |

Each needs a reason to change. Every 5xx counts, including maintenance mode's 503: during planned maintenance longer than the window, disable the job (`PUT /ops/jobs/definitions/incidents_detect` with `{"enabled": false, "version": …, "reason": …}`) or resolve the incident it opens.

### Reports

`GET /ops/incidents/{id}/report` gathers what happened from 15 minutes before the incident started until 15 minutes after it was resolved (or now), at most 24 hours:

| Section | Needs | Holds |
|---|---|---|
| Incident and timeline | `ops.incidents.read` | Everything above |
| `requests` | `ops.observability.read` | Totals, error rate and latency; each instance; the 10 routes with the most server errors; each minute |
| `audit_events` | `ops.audit.read` | Up to 200 events in the window: time, action, outcome, actor kind and ID, resource and request ID. Never IP addresses, user agents, actor labels or metadata |
| `releases` | `ops.releases.read` | Builds instances started in the window, with how many starts: did a deploy line up with the incident? |

A section the caller can't read is left out with a note, so `ops.incidents.read` can be granted without widening access to the audit log. Request counts older than `observability.retention` are gone, so report on an incident while its window is still kept, or raise the retention.

Ask for Markdown to paste into a postmortem:

```bash
curl -H "Authorization: Bearer $TOKEN" 'http://127.0.0.1:8080/ops/incidents/12/report?format=markdown'
curl -H "Authorization: Bearer $TOKEN" -H 'Accept: text/markdown' http://127.0.0.1:8080/ops/incidents/12/report
```

Text stored from requests (titles, messages, versions, resource IDs) is escaped in the Markdown, so it can't add HTML or formatting to the page that renders it. The Markdown lists at most the 60 minutes with the most server errors; the JSON has every minute.

## In your own code

The collector is wired in `internal/app/observability.go` and the middleware chain in `routes.go`. Keep the router wrapped with `observability.RecordRoute(mux)`: without it, requests whose middleware copies the request (such as authentication) have no route. Register routes with patterns (`GET /v1/items/{id}`), never one handler per path.

To count requests served another way, call `collector.Record(observability.Request{…})`. To watch requests as they finish (such as a development console), `collector.Subscribe(fn)` calls `fn` with each request's method, route, status, duration, path without query, request ID and trace ID; `fn` runs on the request's goroutine, so it must not block.
