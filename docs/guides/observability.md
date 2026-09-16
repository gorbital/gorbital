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
