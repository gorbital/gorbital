# Ops API reference

Admin APIs of the Full preset (`internal/modules/ops`), implemented in `examples/full-single`. The full schema is in the app's `api/openapi.json` and at `/docs`. Decisions: [ADR-0026](../adr/0026-operations-apis.md), [ADR-0031](../adr/0031-runtime-settings.md), [ADR-0033](../adr/0033-background-jobs.md), [ADR-0036](../adr/0036-audit-storage.md), [ADR-0037](../adr/0037-email-setup-and-delivery.md), [ADR-0038](../adr/0038-authentication-v0-2.md), [ADR-0040](../adr/0040-release-tracking.md), [ADR-0064](../adr/0064-live-observability-and-incidents.md).

## Authentication

`/ops/*` requests use a signed-in session: a browser's session cookie, or a bearer token from `POST /v1/auth/login` with `"transport": "bearer"`. The account needs a platform role ([authentication guide](authentication.md)):

```bash
go run ./cmd/api grant-role you@example.com platform_admin
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"your password","transport":"bearer"}' | jq -r .token)
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/ops/settings
```

| Situation | Response |
|---|---|
| No or invalid session | 401 `unauthenticated` |
| Signed in without the operation's permission | 403 `forbidden` |
| Role `platform_admin` | Every ops permission |
| Role `ops_viewer` | `ops.settings.read`, `ops.jobs.read`, `ops.audit.read`, `ops.releases.read`, `ops.mail.read`, `ops.auth.read`, `ops.system.read`, `ops.observability.read`, `ops.incidents.read` |

Changes are attributed to the signed-in user in history, job metadata and audit events.

## Permissions

| Permission | Allows |
|---|---|
| `ops.settings.read` | List and read settings and their history |
| `ops.settings.write` | Change and reset settings |
| `ops.jobs.read` | Read job definitions, scheduled jobs, runs and queues |
| `ops.jobs.write` | Change and reset job configuration; pause and resume queues |
| `ops.jobs.run` | Run a job now, retry or cancel a run |
| `ops.audit.read` | List and read audit events |
| `ops.releases.read` | List releases and the instances running them |
| `ops.mail.read` | See how the app sends email |
| `ops.mail.test` | Send a test email |
| `ops.mail.write` | Remove addresses from the email suppression list |
| `ops.auth.read` | See which sign-in methods are configured |
| `ops.system.read` | See an instance's health checks, database pool, migrations and runtime |
| `ops.observability.read` | See request rates, errors and latency across instances, and stream them |
| `ops.incidents.read` | Read incidents, their timelines and reports |
| `ops.incidents.write` | Open, update and resolve incidents |

Missing permission: 403 `forbidden`.

## Conventions

- Errors are `application/problem+json` with `code`, `detail` and `request_id`.
- Changes send the `version` they last read; a newer version returns a `*_version_conflict` error. Read again and retry.
- Unknown request fields are ignored.

## Runtime settings

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/settings?group=` | List settings | 200 `{settings: [...]}` |
| `GET /ops/settings/{key}` | One setting | 200 |
| `PUT /ops/settings/{key}` | Change: `{value, version, reason?}` | 200 |
| `DELETE /ops/settings/{key}` | Reset to default: `{version, reason?}` | 200 |
| `GET /ops/settings/{key}/history?before=&limit=` | Changes, newest first | 200 `{changes: [...]}` |
| `GET /ops/settings/{key}/overrides?after=&limit=` | Organisations' own values of a setting with `org_overridable`, by organisation ID ([ADR-0056](../adr/0056-per-organisation-settings.md)); always empty in single-tenant apps | 200 `{overrides: [{org_id, value, invalid_stored_value, version, updated_at, updated_by}]}` |

Settings in the Full apps (`internal/app/settings.go`; the list is recorded in `api/surface.json`). Keys are public API. A change to a setting marked **Required** without `reason` answers 422 `setting_reason_required`.

| Key | Default | Bounds | Reason | What it controls |
|---|---|---|---|---|
| `example.ping_message` | `pong` | up to 100 characters | Optional | Reply of `GET /v1/ping` |
| `mail.from_name` | the app's name | up to 100 characters, one line | Required | Sender name on every email |
| `mail.from_email` | `no-reply@example.com` | email address | Required | Sender address on every email |
| `mail.reply_to` | empty | empty or email address | Required | Where replies go |
| `auth.session_idle_ttl` | 14 days | 1 hour to 90 days | Required | Session lifetime without use |
| `auth.session_absolute_ttl` | 90 days | 1 to 365 days | Required | Longest session lifetime |
| `auth.verification_code_ttl` | 15 minutes | 5 minutes to 1 hour | Required | Email verification code lifetime |
| `auth.reset_code_ttl` | 30 minutes | 10 minutes to 2 hours | Required | Password reset code lifetime |
| `auth.deleted_account_retention` | 30 days | 1 to 365 days | Required | Deleted accounts kept before `auth_cleanup` removes them |
| `auth.unverified_account_ttl` | 7 days | 1 hour to 90 days | Required | Never-verified accounts kept before `auth_cleanup` deletes them (Google and Apple accounts kept) |
| `auth.ip_requests_per_minute` | 60 | 10 to 10,000 | Required | Changing `/v1/auth/` requests per client IP (IPv6 /64) |
| `auth.login_attempts` | 10 | 3 to 100 | Required | Sign-in attempts per address per client network |
| `auth.login_address_attempts` | 50 | 10 to 1,000 | Required | Sign-in attempts per address from all networks |
| `auth.login_window` | 15 minutes | 1 minute to 24 hours | Required | Window of the sign-in, 2FA change and re-authentication limits |
| `auth.mfa_change_attempts` | 10 | 3 to 100 | Required | Two-factor changes per user |
| `auth.reauth_attempts` | 10 | 3 to 100 | Required | Password or second-factor checks per signed-in user (password change, 2FA setup, passkeys, linking, deletion) |
| `auth.code_attempts` | 20 | 5 to 100 | Required | Verification and reset code checks per address, across codes |
| `auth.code_window` | 24 hours | 1 hour to 7 days | Required | Window of `auth.code_attempts` |
| `audit.retention` | 365 days | 30 days to 10 years | Required | Audit events kept |
| `ops.history_retention` | 365 days | 30 days to 10 years | Required | Setting and job configuration history kept |
| `releases.instance_retention` | 90 days | 1 day to 3 years | Optional | Instances listed after last seen |
| `maintenance.enabled` | `false` | — | Required | Maintenance mode |
| `maintenance.message` | empty | up to 500 characters | Optional | Message during maintenance |
| `maintenance.retry_after` | 5 minutes | 1 minute to 24 hours | Optional | `Retry-After` during maintenance |
| `orgs.invitation_url` (multi-tenant) | `http://localhost:3000/invitations` | http(s) URL without fragment, up to 500 characters | Required | Frontend page invitation links open |
| `orgs.invitation_ttl` (multi-tenant) | 7 days | 1 to 30 days | Required | Invitation link lifetime |
| `orgs.deleted_org_retention` (multi-tenant) | 30 days | 1 to 365 days | Required | Deleted organisations restorable before `orgs_purge` |
| `orgs.max_owned` (multi-tenant) | 20 | 1 to 10,000 | Required | Organisations one user may own, personal workspace aside |
| `orgs.user_invitations_per_hour` (multi-tenant) | 50 | 1 to 10,000 | Required | Invitations per user per hour across organisations |

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/example.ping_message \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"hello","version":0,"reason":"demo"}'
```

```json
{
  "key": "example.ping_message",
  "kind": "string",
  "group": "example",
  "description": "Reply of GET /v1/ping. …",
  "value": "hello",
  "default": "pong",
  "modified": true,
  "invalid_stored_value": false,
  "version": 1,
  "updated_at": "2026-09-14T12:00:00Z",
  "updated_by": "usr_mfrggzdfmztwq2lk",
  "reason_required": false,
  "restart_required": false,
  "restart_pending": false,
  "constraints": {"max_len": 100},
  "org_overridable": false
}
```

## Retention

How long data is kept is a runtime setting per kind of data ([ADR-0051](../adr/0051-operations-v0-5.md)), so changes need the setting's reason, appear in its history and audit log, and apply on every instance without a restart. `GET /ops/retention` (`ops.settings.read`) lists them:

| Data | Setting | Default | Deleted by |
|---|---|---|---|
| `audit_events` | `audit.retention` | 365 days (30 days to 10 years) | `retention` job, daily at 04:15 |
| `settings_history`, `job_definition_history` | `ops.history_retention` | 365 days (30 days to 10 years) | `retention` job |
| `release_instances` | `releases.instance_retention` | 90 days (1 day to 3 years) | each instance, when it starts |
| `deleted_accounts` | `auth.deleted_account_retention` | 30 days | `auth_cleanup` job |
| `observability_minutes` | `observability.retention` | 24 hours (1 hour to 7 days) | `observability_cleanup` job, hourly ([observability](observability.md)) |
| `idempotency_keys` | `idempotency.retention` | 24 hours (1 hour to 7 days) | `idempotency_cleanup` job, hourly ([idempotency](idempotency.md)) |
| `deleted_organisations` (multi-tenant apps) | `orgs.deleted_org_retention` | 30 days | `orgs_purge` job |

Each policy shows `retention` (a Go duration) and `retention_seconds`, the enforcing `job` with its `last_run` and `next_run_at`, and `oldest_at` for the data the `retention` job deletes. The job deletes 5,000 rows per statement until nothing is older, and records a `retention.purged` audit event with the row count and cutoff for each kind of data, so a shortened retention stays visible after the rows are gone.

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/audit.retention \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"17520h","version":0,"reason":"two-year compliance requirement"}'
```

## Maintenance mode

Turn it on with the `maintenance.enabled` setting (a reason is required); every instance applies it within a second ([ADR-0051](../adr/0051-operations-v0-5.md)). While it's on, requests answer 503 with problem code `maintenance`, `detail` set to `maintenance.message` (a generic message when empty) and a `Retry-After` header from `maintenance.retry_after` (5 minutes by default).

Still served, so load balancers keep instances in rotation and staff can turn it off: `/livez`, `/readyz`, `/version`, `/openapi.json`, `/docs`, `/.well-known/*`, `/ops/*` and `/v1/auth/*`. Sign-in works for everyone, but every product route after it answers 503.

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/maintenance.message \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"Upgrading the database until 10:00 UTC","version":0}'
curl -X PUT http://127.0.0.1:8080/ops/settings/maintenance.enabled \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":true,"version":0,"reason":"database upgrade"}'
```

When nobody can reach `/ops`, run it where the app's environment is set:

```bash
go run ./cmd/api maintenance on --message "Back at 10:00 UTC"
go run ./cmd/api maintenance off
```

The command writes the same settings as `system:cli`, recorded in their history and the audit log.

## Job definitions

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/jobs/definitions` | All definitions with effective and default config, next and last run | 200 `{definitions: [...]}` |
| `GET /ops/jobs/scheduled` | Enabled scheduled jobs, soonest first | 200 `{definitions: [...]}` |
| `GET /ops/jobs/definitions/{name}` | One definition | 200 |
| `PUT /ops/jobs/definitions/{name}` | Change only the sent fields: `{enabled?, schedule?, timeout?, max_attempts?, queue?, priority?, version, reason?}` | 200 |
| `DELETE /ops/jobs/definitions/{name}` | Reset to code defaults: `{version, reason?}` | 200 |
| `GET /ops/jobs/definitions/{name}/history?before=&limit=` | Changes: `action`, `old_config`, `new_config`, reason, actor | 200 `{changes: [...]}` |
| `POST /ops/jobs/definitions/{name}/run` | Run now; refused (429 `job_run_limited`) while a run of the job is queued or running, or within a minute of its last run | 202 with the run |

```bash
curl -X PUT http://127.0.0.1:8080/ops/jobs/definitions/heartbeat \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"schedule":"@every 2h","timeout":"30s","version":0,"reason":"less noise"}'
```

```json
{
  "name": "heartbeat",
  "description": "Logs a heartbeat. …",
  "config":   {"enabled": true, "schedule": "@every 2h", "timeout": "30s", "max_attempts": 3, "queue": "default", "priority": 1},
  "defaults": {"enabled": true, "schedule": "@every 1h", "timeout": "1m0s", "max_attempts": 3, "queue": "default", "priority": 1},
  "modified": true,
  "invalid_override": false,
  "version": 1,
  "updated_by": "usr_mfrggzdfmztwq2lk",
  "next_run_at": "2026-09-14T14:00:00Z",
  "last_run": {"id": 42, "kind": "heartbeat", "state": "completed", "attempt": 1, "…": "…"}
}
```

A reason is required to disable a job, change an enabled job's schedule, or change its `timeout`, `max_attempts` or `queue`: each can stop the job doing its work while it still looks enabled. Enabling a job and changing its priority don't need one.

## Job runs

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/jobs/runs?kind=&queue=&state=&limit=&cursor=` | Runs, newest first; `state` is comma-separated: available, cancelled, completed, discarded, pending, retryable, running, scheduled | 200 `{jobs: [...], next_cursor?}` |
| `GET /ops/jobs/runs/{id}` | One run with attempt errors | 200 |
| `POST /ops/jobs/runs/{id}/retry` | Make a run that is waiting to retry, discarded or cancelled run again now. Completed, queued and running runs return 409 `job_not_retryable`, so a delivered email or finished purge never runs twice; runs of a disabled job return 409 `job_definition_disabled` | 200 |
| `POST /ops/jobs/runs/{id}/cancel` | Cancel; a running job's context is cancelled | 200 |

Run fields: `id`, `kind`, `queue`, `state`, `attempt`, `max_attempts`, `priority`, `created_at`, `scheduled_at`, `attempted_at`, `finalized_at`, `errors[{at, attempt, message}]`, `request_id`, `actor_kind`, `actor_id`. Arguments are never returned.

`GET /ops/jobs/overview` (`ops.jobs.read`, [ADR-0051](../adr/0051-operations-v0-5.md)) summarises job work across every instance:

```json
{
  "queues": [
    {"name": "default", "active": true, "paused": false,
     "available": 0, "scheduled": 3, "running": 1, "retryable": 2, "discarded_last_day": 1}
  ],
  "failing": [{"name": "send_digest", "last_run": {"state": "retryable", "errors": [...]}, ...}]
}
```

`queues` lists queues with active workers or unfinished jobs; `failing` lists job definitions, in the same shape as `GET /ops/jobs/definitions`, whose most recent run is retrying or was discarded.

## Queues

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/queues` | Active queues: `name`, `paused`, `paused_at`, `created_at`, `updated_at` | 200 `{queues: [...]}` |
| `POST /ops/queues/{name}/pause` | Stop every instance fetching from the queue: `{reason}`, required | 204 |
| `POST /ops/queues/{name}/resume` | Resume it: `{reason?}` | 204 |

Pausing a queue stops every job in it, email delivery included, so it needs a reason; the reason is recorded in the `jobs.queue.paused` audit event.

## Audit log

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/audit?actor_kind=&actor_id=&action=&action_prefix=&resource_type=&resource_id=&org_id=&outcome=&request_id=&from=&to=&limit=&cursor=` | Events, newest first; filters combine; `from` (inclusive) and `to` (exclusive) are RFC 3339 times compared with `occurred_at` | 200 `{events: [...], next_cursor?}` |
| `GET /ops/audit/{id}` | One event | 200 |
| `GET /ops/audit/stats?group_by=&from=&to=&actor_kind=&actor_id=&action=&action_prefix=&resource_type=&org_id=&outcome=` | Counts in a window of at most 90 days (default the last 7), grouped by `action`, `outcome`, `actor_kind`, `resource_type` or `day` (UTC) | 200 `{from, to, group_by, total, groups: [{key, count}], other}` |

Stats return the 50 largest groups, with the rest counted in `other`; `group_by=day` returns every day with events, in order. A wider window or an unknown grouping returns 422 `invalid_audit_filter`.

Listings and stats stop after 5 seconds with 503 `audit_query_timeout`. Filters on `actor_id`, `action`, `resource_type`/`resource_id`, `org_id` and `request_id` use an index; `outcome`, `actor_kind` or `action_prefix` alone scan events newest first, so on a large table add one of the indexed filters or a `from`/`to` window.

```bash
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8080/ops/audit/stats?group_by=action&action_prefix=auth.&outcome=failure'
```

```bash
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8080/ops/audit?action_prefix=settings.&limit=20'
```

```json
{
  "events": [
    {
      "id": 7,
      "occurred_at": "2026-09-14T12:00:00.123Z",
      "recorded_at": "2026-09-14T12:00:00.125Z",
      "actor_kind": "user",
      "actor_id": "usr_mfrggzdfmztwq2lk",
      "action": "settings.value.changed",
      "resource_type": "setting",
      "resource_id": "example.ping_message",
      "outcome": "success",
      "request_id": "req_99c4a38756b2eb8f",
      "trace_id": "951ff1fe97c8f8616496d020314fea38",
      "metadata": {"reason": "demo", "reset": false, "version": 1}
    }
  ],
  "next_cursor": "7"
}
```

- `request_id` links an event to its access log line, trace and any jobs the request enqueued.
- `ip` and `user_agent` appear on every event recorded during a request, such as sign-ins and ops changes, and are empty for events recorded by jobs and commands.
- Metadata values under sensitive keys such as `password`, `tokens`, `apiKeys` or `recovery_codes` are stored as `"[REDACTED]"` (singular or plural, any case style); oversized metadata is replaced with `{"metadata_dropped": "too_large"}`.
- Events can't be changed. The `retention` job deletes events older than `audit.retention` (365 days by default) and records a `retention.purged` event for each deletion: see [Retention](#retention).

## Releases

Every instance records its build when it starts (version, commit, build time, whether the tree had uncommitted changes, Go version, host), sends a heartbeat every 30 seconds and marks itself stopped when it shuts down cleanly. An instance is **running** until it stops or misses three heartbeats, so crashed instances drop out after about 90 seconds. Instances last seen more than 90 days ago are deleted.

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/releases?limit=&cursor=` | Releases (one per version and commit), newest first: `first_started_at`, `last_seen_at`, `running`, `starts`, `modified` | 200 `{releases: [...], next_cursor?}` |
| `GET /ops/releases/current` | Releases running now, each with its running instances; more than one during a rolling deploy | 200 `{releases: [...]}` |
| `GET /ops/releases/instances?version=&commit=&running=&limit=&cursor=` | Instance starts, newest first | 200 `{instances: [...], next_cursor?}` |

```bash
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/ops/releases/current
```

```json
{
  "releases": [
    {
      "version": "v1.4.0",
      "commit": "3f9a1c2b7d4e8a90",
      "instances": [
        {"id": 12, "instance_id": "9b1c…", "version": "v1.4.0", "commit": "3f9a1c2b7d4e8a90",
         "build_time": "2026-09-15T09:58:00Z", "modified": false, "go_version": "go1.26.1", "host": "acme-api-7d9f8-x2kq",
         "started_at": "2026-09-15T10:02:11Z", "last_seen_at": "2026-09-15T10:31:41Z", "running": true}
      ]
    }
  ]
}
```

Builds without version control information or a link-time version show `"version": "dev"` and no commit.

## System

`GET /ops/system` describes the instance that answers ([ADR-0051](../adr/0051-operations-v0-5.md)); behind a load balancer, repeat it to reach others, and use `GET /ops/releases/instances` for the whole fleet. It never includes the database URL, dependency host names, environment variables or settings values.

| Section | Fields |
|---|---|
| `instance` | `id` (matches `instance_id` in `/ops/releases/instances`), `version`, `commit`, `build_time`, `modified`, `started_at`, `uptime_seconds` |
| `checks` | Each readiness check (as `/readyz` runs it): `name`, `status` (`ok` or `error`), `duration_ms` |
| `database` | `status`, `error` (a fixed description, never the driver's message), `ping_ms`; `pool`: `total`, `idle`, `in_use`, `max`, `acquires`, `average_acquire_ms`, `empty_acquires` (waited for a connection), `canceled_acquires`; `migrations`: `current`, `latest`, `pending` |
| `runtime` | `go_version`, `gomaxprocs`, `goroutines`, `heap_in_use_bytes`, `last_gc_pause_ms`, `gcs` |
| `jobs` | `workers` this instance runs and its `queues` |

A failing database still returns 200, with `database.status` and the `postgres` check set to `error`. `migrations.pending` above 0 means this build's migrations haven't been applied: run `go run ./cmd/migrate`.

```bash
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/ops/system
```

## Observability

Request rates, errors and latency across every instance ([ADR-0064](../adr/0064-live-observability-and-incidents.md), [observability guide](observability.md)). Each instance counts its requests per minute and route pattern and writes them to PostgreSQL every 15 seconds, so the latest minute can lag by that much. Permission `ops.observability.read` (`ops_viewer`, `platform_admin`).

| Endpoint | Purpose | Success |
|---|---|---|
| `GET /ops/observability/overview` | Query `window` (whole minutes from `1m` to `24h`, default `15m`; 422 `invalid_observability_window`). Totals over the window up to the current minute: `requests`, `requests_per_minute`, `client_errors`, `server_errors`, `error_rate` (server errors per request), `latency_ms` (`mean`, `p50`, `p95`, `p99` estimated, `max` exact); `instances` (by `instance_id`, with `last_minute` and `last_write`); `top_routes` (`by_requests`, `by_errors`, `by_latency`, 10 each); `minutes` (requests, server errors and p95 per minute) | 200 |
| `GET /ops/observability/routes` | Every route over `window`, sorted by `sort` (`requests`, `errors`, `error_rate`, `p95`, `p99`), at most `limit` (1–500, default 100) | 200 `{window, from, to, routes}` |
| `GET /ops/observability/stream` | Server-Sent Events: `retry: 5000`, an `overview` event now and every 5 seconds (the overview's body), and a last `end` event with `reason` (`max_duration` after 10 minutes, `unauthorized` when the session ends or loses the permission, `shutting_down`, `unavailable`). 2 streams per user and 20 per instance (429 `observability_streams_limited`) | 200 `text/event-stream` |

Routes are the patterns registered in code (`/v1/projects/{id}`), with the method; `""` is requests no route matched (such as maintenance-mode 503s and rate-limited requests, answered before routing), `_overflow` requests beyond 500 routes a minute. Requested paths and query strings are never stored. A query over a long window that takes more than 5 seconds answers 503 `observability_query_timeout`.

```bash
curl -H "Authorization: Bearer $TOKEN" 'http://127.0.0.1:8080/ops/observability/overview?window=1h'
curl -N -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/ops/observability/stream
```

## Incidents

Incidents record what went wrong, when, and what operators did, as a timeline ([ADR-0064](../adr/0064-live-observability-and-incidents.md)). Operators open them; the `incidents_detect` job opens automatic ones when the server error rate crosses `incidents.error_rate_threshold` ([observability guide](observability.md#automatic-incidents)). Reading needs `ops.incidents.read` (`ops_viewer`, `platform_admin`), changes `ops.incidents.write` (`platform_admin`).

| Endpoint | Purpose | Success |
|---|---|---|
| `POST /ops/incidents` | Open: `{title, severity, summary?, status?, started_at?, message?}`. `severity` `sev1`–`sev4`; `status` `investigating` (default), `identified` or `monitoring`; `started_at` up to 90 days ago (default now). Audit `ops.incident.opened` | 201 with the incident and its timeline |
| `GET /ops/incidents` | Most recently opened first. Query `status` (a status, or `open` for every status but resolved), `severity`, `source` (`manual`, `automatic`), `started_from`, `started_to`, `limit` (1–100, default 50), `cursor` | 200 `{incidents, next_cursor}` |
| `GET /ops/incidents/{id}` | The incident with its `updates`, oldest first | 200 |
| `POST /ops/incidents/{id}/updates` | A timeline update: `{message, status?, severity?}`; status and severity change when given. Audit `ops.incident.updated` | 201 with the incident and its timeline |
| `POST /ops/incidents/{id}/resolve` | `{message}`: status `resolved`, `resolved_at` now. Audit `ops.incident.resolved` | 200 |
| `GET /ops/incidents/{id}/report` | The report: `format=json` (default) or `format=markdown`, or `Accept: text/markdown` | 200 |

An incident has `id`, `title`, `summary`, `severity`, `status`, `source`, `started_at`, `resolved_at`, `recovered_at` (automatic incidents, while detection sees the error rate recovered), `created_by` (`kind` and `id`), `created_at`, `updated_at`. Each update has `kind` (`opened`, `update`, `resolved`, or `recovered` and `breaching` from detection), `message`, the `status` and `severity` after it, `actor` and `created_at`. Resolved incidents can't change (409 `incident_resolved`); an incident keeps at most 500 updates (409 `incident_updates_limited`).

The report covers 15 minutes before the incident started until 15 minutes after it was resolved, or now, at most 24 hours. Besides the incident and its timeline, each section needs its own read permission, and a section the caller can't read (or that failed) is left out with a `*_note`:

| Section | Needs | Holds |
|---|---|---|
| `requests` | `ops.observability.read` | Totals, latency, `instances`, `error_routes` (10 with the most server errors) and `minutes` of the window |
| `audit_events` | `ops.audit.read` | Up to 200 events, newest first: `id`, `occurred_at`, `action`, `outcome`, `actor`, `resource_type`, `resource_id`, `request_id`. Never IP addresses, user agents, actor labels or metadata: read those with `GET /ops/audit/{id}` |
| `releases` | `ops.releases.read` | Builds instances started in the window: `version`, `commit`, `modified`, `first_started_at`, `starts` |

The Markdown version has the same sections as tables; titles, messages and other stored text are escaped so they can't add Markdown or HTML to a page that renders the report.

```bash
curl -X POST http://127.0.0.1:8080/ops/incidents -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Checkout requests failing","severity":"sev1","summary":"Payments time out."}'
curl -H "Authorization: Bearer $TOKEN" -H 'Accept: text/markdown' http://127.0.0.1:8080/ops/incidents/1/report
```

## Email

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/mail` | Provider (`resend` or `smtp`), delivery (`mailpit` or `provider`), non-secret `details` from the environment, and the current `from_name`, `from_email`, `reply_to` | 200 |
| `POST /ops/mail/test` | Queue a test email: `{to}`. Each operator can send 5 an hour (429 `rate_limited`) | 202 `{status: "queued", to, delivery}` |
| `GET /ops/mail/suppressions` | Suppressed addresses, most recently added first: `id`, `email`, `reason` (`bounce` or `complaint`), `source`, `detail`, `created_at`, `updated_at`. Query `reason`, `limit` (1–100, default 50), `cursor` (400 `invalid_cursor`). Needs `ops.mail.read` | 200 `{suppressions, next_cursor}` |
| `DELETE /ops/mail/suppressions/{id}` | Remove a suppressed address: `{reason}` (required, 422 `mail_suppression_reason_required`; 404 `mail_suppression_not_found`). Audit event `mail.suppression.removed` with the reason, without the address. Needs `ops.mail.write` | 200 with the removed suppression |

```json
{"provider": "smtp", "delivery": "provider", "details": {"host": "smtp.postmarkapp.com", "port": "587", "tls": "starttls", "auth": "username and password"},
 "from_name": "Acme", "from_email": "hello@acme.com", "reply_to": "support@acme.com"}
```

Resend details are `{"api_key": "configured", "webhook_secret": "missing"}` (each `configured` or `missing`). Change the sender with `PUT /ops/settings/mail.from_email` (and `mail.from_name`, `mail.reply_to`), with a reason. The test email's delivery appears in `GET /ops/jobs/runs?kind=gorbital.mail.send`. Suppressions come from Resend's signed webhook `POST /v1/webhooks/resend` (hard bounces and complaints); an email to a suppressed address is cancelled. Setup: [email guide](email.md).

## Sign-in methods

Which sign-in methods this deployment has configured, and what turns the others on ([ADR-0045](../adr/0045-sign-in-provider-setup.md), [sign-in provider setup](auth-providers.md)). Permission `ops.auth.read` (`ops_viewer`, `platform_admin`).

| Endpoint | Purpose | Success |
|---|---|---|
| `GET /ops/auth/providers` | Each method: `key` (`email_password`, `authenticator_app`, `passkeys`, `passkeys_ios`, `passkeys_android`, `google`, `google_ios`, `google_android`, `apple`, `apple_ios`), `name`, `enabled`, `detail` for enabled methods (relying party ID and origins, app IDs, Android packages), `missing` environment variables and the `guide` section for the others | 200 `{methods: [...]}` |

Values of secrets are never returned; the same report is printed at start in development and by `go run ./cmd/api auth-providers`.

## Error codes

| Code | Status | When |
|---|---|---|
| `unauthenticated` | 401 | No valid session |
| `forbidden` | 403 | Missing permission |
| `not_found` | 404 | No such route |
| `validation_failed` | 422 | Request doesn't match the schema |
| `setting_not_found` | 404 | Unknown setting key |
| `setting_version_conflict` | 409 | Setting changed since it was read |
| `setting_reason_required` | 422 | Reason missing for a setting that requires one |
| `invalid_setting_value` | 422 | Value fails the setting's type or validation; `detail` says why |
| `job_definition_not_found` | 404 | Unknown job name |
| `job_definition_version_conflict` | 409 | Definition changed since it was read |
| `job_reason_required` | 422 | Reason missing to disable or reschedule a job, change its timeout, attempts or queue, or pause a queue |
| `invalid_job_config` | 422 | Schedule, timeout, attempts or priority out of bounds; `detail` says why |
| `job_definition_disabled` | 409 | Run now, or retry a run, of a disabled job |
| `job_run_limited` | 429 | Run now while a run is queued or running, or within a minute of the last run |
| `job_not_retryable` | 409 | Retry of a run that isn't waiting to retry, discarded or cancelled |
| `job_not_found` | 404 | Unknown run ID (or removed by retention) |
| `queue_not_active` | 422 | No worker runs that queue |
| `invalid_cursor` | 400 | Malformed `cursor` |
| `invalid_job_state` | 422 | Unknown `state` filter |
| `audit_event_not_found` | 404 | Unknown audit event ID (or removed by retention) |
| `invalid_audit_filter` | 422 | Unknown `outcome`, malformed `action_prefix`, or `from` not before `to` |
| `audit_query_timeout` | 503 | An audit listing or stats query took longer than 5 seconds; narrow the filters or the window |
| `invalid_recipient` | 422 | The test email recipient isn't an email address |
| `rate_limited` | 429 | More than 5 test emails an hour from one operator |
| `invalid_observability_window` | 422 | `window` isn't whole minutes from `1m` to `24h` |
| `observability_query_timeout` | 503 | Reading request counts took longer than 5 seconds; use a shorter window |
| `observability_streams_limited` | 429 | The user already holds 2 streams on the instance, or the instance 20 |
| `incident_not_found` | 404 | Unknown incident ID |
| `invalid_incident` | 422 | Title, summary, severity, status, start time or message missing or out of bounds |
| `incident_resolved` | 409 | Update or resolve of a resolved incident |
| `incident_updates_limited` | 409 | The incident has 500 updates |
| `maintenance` | 503 | Maintenance mode is on; `detail` is `maintenance.message` and `Retry-After` is set. Any route but health checks, docs, sign-in and `/ops` |

Error codes are public API: new ones are added, existing ones never change.

## Audit actions

The ops APIs record these. Every action a Full app records, with its metadata: [audit actions reference](../reference/audit-actions.md).

| Action | Resource |
|---|---|
| `settings.value.changed` | `setting` |
| `jobs.definition.changed` | `job_definition` |
| `jobs.definition.run_requested` | `job_definition` |
| `jobs.run.retried` | `job` |
| `jobs.run.cancelled` | `job` |
| `jobs.queue.paused` | `job_queue` (metadata: `reason`) |
| `jobs.queue.resumed` | `job_queue` |
| `mail.test.requested` | `mail` (ID: the provider; the recipient is not recorded) |
| `ops.incident.opened`, `ops.incident.updated`, `ops.incident.resolved` | `incident` (metadata: `source`, `status`, `severity`, `update_id`, `update_kind`; never titles or messages) |

`examples/full-single` stores these events in the `audit_events` table (`modules/auditpg`) and lists them with `GET /ops/audit`. Reading the audit log is not itself audited.
