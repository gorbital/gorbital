# 18. Audit, logs and observability

Plateful works. Somebody will now ask you three questions that sound like one question:

- *"What is the API doing right now?"* — a customer says checkout is slow.
- *"Who suspended Trattoria Bruno, and why?"* — Bruno is on the phone.
- *"Why did that one request fail?"* — there is a `request_id` in a support ticket.

Three questions, three different mechanisms, three different retentions, three different sets of rules about who may read them. This chapter separates them, because the most common mistake in an app this size is to answer all three with one pile of log lines.

| | **Logging** | **Audit** | **Observability** |
|---|---|---|---|
| Answers | Why did *this* request behave that way | Who changed *this* thing, and when | How is the API doing *right now* |
| Written by | `slog`, from `Deps.Logger` | `audit.Recorder`, from `Deps.Audit` | A per-instance collector, automatically |
| Stored in | Standard output, wherever you ship it | The `audit_events` table in PostgreSQL | The `observability_minutes` table |
| Kept for | Whatever your log pipeline keeps | `audit.retention`, 365 days by default | `observability.retention`, 24 hours by default |
| Read by | You, with `grep` | Operators, through `GET /ops/audit` | Operators, through `GET /ops/observability/overview` |
| Holds personal data | IDs, paths, addresses — never emails, tokens or secrets | Actor IDs, IP addresses, user agents, redacted metadata | Nothing about anyone: counts per route |

Depth for each lives in its own guide: [observability and incidents](../guides/observability.md), the [ops API reference](../guides/ops-api.md), and the [audit actions reference](../reference/audit-actions.md). This chapter is about what Plateful does and why.

## Logging

**What we're doing.** Making sure every line Plateful writes can be tied back to the request that wrote it.

**Why.** A log line that says `record orders audit event` and nothing else is useless at three in the morning. A log line that carries `trace_id`, `request_id`, `org_id` and `module` is a starting point: you can find every other line of the same request, the trace of what it called, and the audit event it wrote.

**What the framework already gives us.** All of it, without a line of setup:

- `gorbital.New` builds one `*slog.Logger` and hands each module its own copy through `Deps.Logger`, tagged with `module` (the name in `module.go`). You never construct a logger.
- The handler under it is wrapped by `telemetry.NewLogHandler`, which adds `request_id`, `trace_id`, `span_id` and `org_id` from the context of any record logged with `…Context`. Tracing is always on — the default sample ratio is 1, and spans exist whether or not `OTEL_EXPORTER_OTLP_ENDPOINT` is set — so `trace_id` and `span_id` are on every line, not just when a vendor is configured.
- `httpx.AccessLog`, in the middleware stack, writes one `http request` line per request with `source`, `method`, `path`, `route`, `status`, `duration_ms`, `bytes`, `request_id` and the signed-in user. Query strings and bodies are never logged.
- `APP_LOG_FORMAT` chooses the encoding: JSON in production, text in development, and `orb dev` sets JSON so the Dev Portal's Logs screen can index it while still printing text to your terminal.

**What we build ourselves.** Nothing structural. We only choose *what* to log, and we pass `ctx` so the context attributes arrive.

**How.** Every logging call in Plateful is a `…Context` call with the request's or job's context:

```go
s.logger.ErrorContext(ctx, "record orders audit event", "action", action, "err", err)
```

and the job workers add the job's own identity, because a worker has no request to be identified by:

```go
w.logger.InfoContext(ctx, "late orders reported",
	"job", LateSweepJob, "job_id", job.ID, "org_id", report.OrgID, "orders", len(report.Orders))
```

> **Don't do this**
>
> ```go
> s.logger.Error("record orders audit event", "err", err)   // no ctx
> log.Printf("order %s moved to %s", id, status)            // not slog at all
> ```
>
> **Do this instead:** `s.logger.ErrorContext(ctx, …)`. `slog.Logger.Error` without a context still writes a line, but the handler has no context to read, so the line has no `request_id` and no `trace_id` — it is a line you cannot correlate with anything. `log.Printf` skips the level, the format and the handler entirely.

**What just happened.** One request now produces a small, linked set of records: the access log line, whatever the use case logged, the spans of the SQL it ran, and any audit event it recorded — all carrying the same `request_id` and `trace_id`. The support ticket's `request_id` is enough to find all of it.

## Audit

**What we're doing.** Recording, in the database, the security-relevant things that happened: who suspended a restaurant, who refunded a payment, who hid a review, which job told which restaurant its order was late.

**Why.** Logs are for debugging and they roll away. An audit trail is evidence: it has its own table, its own retention, its own permission (`ops.audit.read`), and it cannot be changed after the fact. Bruno asking "who suspended my restaurant, and why" is not a debugging question.

**What the framework already gives us.** A lot, and knowing how much is the point of this section.

The framework records its own events without being asked. The [audit actions reference](../reference/audit-actions.md) is generated from the code, and the `auth.*` list alone is about fifty actions long — every sign-in and failed sign-in, every password change, every passkey added or removed, every second factor enabled, every role granted, every API key created or revoked, every account banned, deleted or purged. Beside them sit `settings.*` (an operator changed a runtime setting, with the reason they gave), `flags.*`, `jobs.*` (a job definition changed, a run retried or cancelled), `mail.*`, `storage.*`, `orgs.*`, `retention.*` and `ops.*` (incidents opened, updated, resolved). Plateful wrote none of these.

It also gives us the contract for our own: `audit.Recorder`, handed to each module as `Deps.Audit`, and `audit.Event`, whose empty fields `audit.FromContext` fills from the request — the actor, the organisation, the request ID, the trace ID, the client's IP and user agent. Metadata under sensitive keys is stored as `"[REDACTED]"`, and oversized metadata is dropped rather than truncated.

**What we build ourselves.** One small helper per module, and the discipline of calling it. Plateful's modules record thirty-odd actions of their own: `orders.order.accepted`, `payments.payment.refunded`, `reviews.review.hidden`, `notifications.delivery.failed`, and so on. Action names are public API — `TestPublicSurface` refuses to let one disappear — so they are added, never renamed ([chapter 17](17-extending-the-framework.md) covers the surface test).

**How.** The orders module's helper is four lines, and every one of them is deliberate:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/service.go#order-audit -->

Two things to read carefully.

**`context.WithoutCancel(ctx)`.** A customer who taps "cancel order" and closes the app cancels the request's context the moment the connection drops. The order was already cancelled — the transaction committed — so the fact of it must be recorded. Without `WithoutCancel`, the recorder would be handed a cancelled context, the `INSERT` would fail with `context canceled`, and the audit trail would be missing exactly the events for requests that were interrupted. The same applies to a job whose timeout expires between the work and the record.

**The error is logged, not returned.** The event is recorded *after* the change it describes, and the change has already committed. Returning the error would tell the customer their cancellation failed when it succeeded, and retrying would cancel it twice. So a failed audit write becomes an `ERROR` log line — which is a thing you alert on — and the operation still returns success.

> **Don't do this**
>
> ```go
> if err := s.recorder.Record(ctx, e); err != nil {
> 	return domain.Order{}, err     // undoes nothing, reports a failure that didn't happen
> }
> ```
>
> **Do this instead:** log it. The audit write is not part of the transaction and cannot roll it back, so an error from it is an operational problem, not the caller's.

### When the actor isn't in the context

The reviews module has a different problem. `audit.FromContext` fills `OrgID` from the actor — and a customer writing a review belongs to no organisation, while the review belongs to the restaurant's. Leave it to the recorder and the restaurant's own audit trail never shows that anybody reviewed it:

<!-- include examples/apps/plateful/internal/modules/reviews/usecase/service.go#review-audit -->

The payments module ([chapter 15](15-payments-a-rule-across-modules.md)) has the same problem for a different reason: the provider's webhook carries no session at all, so both the actor and the organisation have to be named by hand.

<!-- include examples/apps/plateful/internal/modules/payments/usecase/service.go#payment-service-actor -->

### The one that will bite you: audit from a worker

**Be plain about this, because the failure is silent.** `audit.FromContext` reads the actor from the context. A background job has no request and no actor: nothing called `actor.WithActor` on the context River works a job with. So an event recorded from a worker with the obvious code lands as the *anonymous* actor with an **empty `org_id`** — and an empty `org_id` makes it invisible to every organisation-scoped view of the audit log. Nothing fails. Nothing logs a warning. The event is there, filed under nobody, in the one place an operator most wants the trail to be right.

Plateful's notifications module ([chapter 17](17-extending-the-framework.md)) sets all three fields by hand, and says why in the code:

<!-- include examples/apps/plateful/internal/modules/notifications/usecase/service.go#notification-audit -->

`systemEvent` is what the delivery worker calls for `notifications.delivery.sent` and `notifications.delivery.failed`: the actor is the *system*, identified by the job's own name, and the organisation comes from the job's arguments rather than from a context that does not have it.

The delivery worker ([chapter 13](13-background-jobs.md) built the job it runs in) also shows the second half of auditing a job — *how often*. A failed delivery is retried five times. It records one `notifications.delivery.failed`, on the last attempt, not five:

> Five would not be five failures; they would be one failure described five times, and an audit trail that inflates like that is one nobody reads.

**What just happened.** Plateful's audit trail now answers Bruno's question — `GET /ops/audit?resource_type=restaurant&resource_id=org_…` shows the suspension, the operator who did it and the reason they typed — — the suspension was [chapter 5](05-the-restaurants-module.md)'s platform-only operation — and it answers it the same way whether the change came from a request, from the payment provider's webhook or from a job at four in the morning.

Reading it:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8080/ops/audit?action_prefix=orders.&limit=20'
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8080/ops/audit/stats?group_by=action&action_prefix=notifications.&outcome=failure'
```

The full filter set, the shape of an event and the retention rules are in the [ops API reference](../guides/ops-api.md#audit-log).

## Observability

**What we're doing.** Answering "how is the API doing right now, across every instance" without standing up a metrics backend first.

**Why.** Plateful will run on two or more instances behind a load balancer. `docker logs` on one of them is not an answer, and "set up Prometheus and Grafana" is not an answer on the first day either.

**What the framework already gives us.** Everything; Plateful wrote no observability code at all.

Each instance runs a collector in the middleware stack. It counts every finished request in a one-minute window, keyed by method and **route pattern** — `GET /v1/orgs/{orgId}/orders`, never the path with real IDs in it — and writes its minutes to `observability_minutes` every 15 seconds. Queries add up every instance's rows.

What it counts: requests, client errors (4xx), server errors (5xx, including a handler that panicked), and latency as a sum, a maximum and a histogram. What it never keeps: paths, query strings, headers, client addresses, user IDs, organisation IDs. A client cannot create series, because there is nothing client-chosen in the key.

**Two things to be honest about when you read the numbers.**

*Percentiles are estimates.* `p50`, `p95` and `p99` are interpolated from a histogram of 35 buckets running from 0.25 ms to 60 s. The true value is inside the bucket the estimate lands in, so the error is bounded by the bucket's width: under half a millisecond below 1 ms, and up to 50% between 1 ms and 10 s, where bucket bounds grow by at most 1.5 times. In practice the measured error on realistic latency distributions is a few percent. **`mean` and `max` are exact**, and the histograms add up exactly across instances and minutes, so the error doesn't grow with the window. If you need exact percentiles, that is what an OTLP backend is for; the [observability guide](../guides/observability.md#how-accurate-the-percentiles-are) has the measured numbers.

*Incidents are database rows, not pages.* The `incidents_detect` job runs every minute, adds up every instance's requests over `incidents.detection_window` (5 minutes) and opens a `sev2` incident when the share of server errors crosses `incidents.error_rate_threshold` with at least `incidents.min_requests` requests behind it. At most one automatic incident is open at a time. When the rate recovers it adds a `recovered` update and stops — an operator decides it is over. **Nothing is paged and nothing is posted to chat.** The job writes a row, logs `incident opened: error rate above threshold` at warning level, counts it in the `incidents.detections` metric and records an audit event as `system:incidents_detect`. Alerting on that log line or that metric is your job, and Plateful would use its own `notifications` module for it — which is [chapter 17](17-extending-the-framework.md)'s point about the framework's edges.

**How.** Read it:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8080/ops/observability/overview?window=15m'
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8080/ops/observability/routes?window=1h&sort=p95&limit=20'
curl -N -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8080/ops/observability/stream?window=5m'
```

The overview holds totals, `error_rate`, `latency_ms`, a row per instance, the top routes by requests, errors and latency, and a row per minute. The stream sends the same object as Server-Sent Events every 5 seconds; put it on a wall, and keep response buffering off in front of it.

**What just happened.** Without configuring anything, Plateful can tell you that `POST /v1/restaurants/{restaurantId}/orders` is answering 1,200 requests a minute with a p95 of 61 ms on three instances, that one instance stopped writing 40 seconds ago, and that an incident was opened at 14:52 because the error rate crossed 5%.

## What `/ops` exposes

`opshttp.Module()` in `main.go` is the whole operations surface (the [ops API reference](../guides/ops-api.md#adding-it-to-an-app) lists the wiring). It is worth knowing what is there before you build something that already exists:

| Area | Endpoints | Used for |
|---|---|---|
| Runtime settings | `/ops/settings` | `orders.late_after`, `orders.ordering_paused`, `images.max_bytes` ([chapter 14](14-settings-and-feature-flags.md)) — behaviour changed without a deploy |
| Feature flags | `/ops/flags` | `orders.scheduled_ordering`, `orders.courier_auto_assign`, with per-organisation targeting |
| Jobs | `/ops/jobs/definitions`, `/ops/jobs/runs`, `/ops/queues` | Every attempt of `notification_delivery`, retry, cancel, pause a queue |
| Audit | `/ops/audit`, `/ops/audit/stats` | The section above |
| Observability | `/ops/observability/overview`, `/routes`, `/stream` | The section above |
| Incidents | `/ops/incidents`, `/ops/incidents/{id}/report` | Timelines, and a Markdown postmortem |
| Releases | `/ops/releases/current`, `/ops/releases/instances` | Which build is running where |
| System | `/ops/system` | This instance's readiness checks, pool, **pending migrations**, Go runtime |
| Email | `/ops/mail` | How the app sends, the suppression list, a test send |
| Accounts | `/ops/auth/*`, `/ops/service-accounts` | Roles, sessions, bans, service accounts |
| Storage and retention | `/ops/storage/objects`, `/ops/retention` | Menu photos, and what each kind of data is kept for |
| Maintenance mode | the `maintenance.enabled` setting, or `./cmd/api maintenance on` | Planned downtime: every product route answers 503, while `/ops` and sign-in keep working |

Access needs a signed-in account with a platform role — `platform_admin` for everything, `ops_viewer` for the read-only permissions — and, for those roles, a second factor. `OPS_ALLOWED_IPS` can narrow it further to a VPN range; [chapter 20](20-production-and-deployment.md#ops-allowed-ips) covers the order in which you must set that.

## A production app serves JSON. That is the whole user interface.

Say this out loud before somebody spends a week looking for the admin screen:

- **There is no admin UI.** `/ops` is a JSON API. Operating Plateful in production means `curl`, `jq`, a script, or a small internal tool you write — the [internal admin tool recipe](../examples/recipes/internal-admin-tool.md) is exactly that.
- **`/docs` is the only HTML page the app serves**, and it is the OpenAPI reference for your API, not an admin console. It is **off by default in production** (`APP_DOCS_ENABLED`), along with `/openapi.json`, so a production deployment serves no HTML at all unless you turn it on for a public API reference.
- **The Dev Portal is not part of the app.** It is served by `orb dev`, on your machine, in development only. It refuses to run against an app whose `APP_ENV` is production. Nothing about it ships in the image.

This is a deliberate boundary, not an omission — see [chapter 22](22-where-to-go-from-here.md) for the rest of the list and what to do about each one.

## Next

[19. Testing](19-testing.md): an app per test on real PostgreSQL, the three traps that catch everyone, and the tests that prove what this guide has been claiming.
