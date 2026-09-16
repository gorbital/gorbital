# ADR-0051: Operations in v0.5: audit stats, system health, jobs overview, retention, maintenance mode, API exports and `orb doctor`

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0026, ADR-0027 · **Amended by:** ADR-0053 (reasons, limits and timeouts on ops operations; accepted items), ADR-0064 (a cluster view in `/ops/observability`; `observability_minutes` retention)

## Context

v0.5 finishes the operations work ADR-0026 planned: `GET /ops/audit/stats`, `GET /ops/system`, a jobs overview, retention, maintenance mode, plus the Postman collection and `llms.txt` ADR-0027 planned, and `orb doctor`. Its definition of done: ops endpoints require platform roles and 2FA. ADR-0050 already delivered the upgrade half.

What exists:

| Area | Today | Evidence |
|---|---|---|
| Ops module | Every use case calls `authorize(ctx, permission)` (platform role, 2FA step-up), then a small port over a library store | `internal/modules/ops/usecase/{service,ports}.go` |
| Audit log | List and get with filters; **no stats, no retention: `audit_events` grows forever** | `modules/auditpg`; index `audit_events_occurred_at` exists |
| Jobs | Definitions, runs, queues with pause and resume; finished jobs pruned by River after 1 h / 24 h / 7 d | `modules/jobs` `WithRetention` defaults |
| Other retention | Deleted accounts (`auth.deleted_account_retention`, 30 d), deleted organisations (`orgs.deleted_org_retention`, 30 d), release instances (`releases.WithRetention`, 90 d, not a setting); settings and job-configuration history kept forever | `internal/app/settings.go`, `modules/releases` |
| Health | `/livez`, `/readyz`; `health.Checker.Check` returns each check's status | `health/health.go` |
| Database state | `postgres.Migrations` reports current, latest and pending versions; `pgxpool.Stat` has pool counters | `modules/postgres/migrate.go` |
| Runtime settings | Typed `Bool`, `String`, `Duration`, … live on every instance through LISTEN/NOTIFY, with history and audit events | ADR-0031 |
| API exports | `go run ./cmd/api openapi > api/openapi.json`; a test fails when it's stale; `orb upgrade` regenerates it | `cmd/api/main.go`, `TestOpenAPIUpToDate` |

Constraints: generated apps own their ops module and wiring; the library holds queries and engines (app-owns-modules); every change must reach existing apps through `orb upgrade` (ADR-0050), so no new migration unless needed; ops responses carry no secrets (ADR-0026).

## Decision

### 1. Audit stats: `GET /ops/audit/stats`

- Query: `from`, `to` (default the last 7 days, at most 90 days apart), `group_by` one of `action`, `outcome`, `actor_kind`, `resource_type`, `day`; the same filters as the list (`action_prefix`, `org_id`, `outcome`, …).
- Response: total, and up to 50 groups ordered by count, with `other` for the rest.
- Library: `auditpg.Store.Stats(ctx, StatsFilter)`, one `GROUP BY` bounded by `occurred_at` (uses the existing index), with a 5-second statement timeout.
- Permission: `ops.audit.read`.

### 2. System health: `GET /ops/system`

Describes the instance that answers (other instances: `GET /ops/releases/instances`).

| Section | Fields |
|---|---|
| `instance` | Instance ID, version, commit, build time, started at, uptime |
| `checks` | Each readiness check's name, status and duration (`health.Checker.Check`) |
| `database` | Ping latency; pool total, idle, in use, max, acquire count, average acquire time, times it waited for a connection; migrations current, latest, pending |
| `runtime` | Go version, GOMAXPROCS, goroutines, heap in use, GC pause (last), number of GCs |
| `jobs` | Whether this instance runs the worker, and its queues |

- Never includes the database URL, host names of dependencies, environment variables or settings values.
- Permission: new `ops.system.read` (platform admins and ops viewers).
- A slow or failing database still returns 200 with the failure in `checks` and `database.error`, so operators can see the problem.

### 3. Jobs overview: `GET /ops/jobs/overview`

- Counts of jobs per queue and state (available, scheduled, running, retryable, discarded in the last 24 hours), paused queues, and job definitions whose latest run failed with its error summary.
- Library: `jobs.Manager.Overview(ctx)`, one grouped query over River's job table on its state index.
- Permission: `ops.jobs.read`.

### 4. Retention

Retention periods are **runtime settings**, one per kind of data, so they're changed, bounded, audited and synchronised by the existing machinery:

| Setting | Default | Bounds | Enforced by |
|---|---|---|---|
| `audit.retention` (new) | 365 days | 30 days to 10 years | new `retention` job |
| `ops.history_retention` (new): settings and job-configuration history | 365 days | 30 days to 10 years | new `retention` job |
| `releases.instance_retention` (new, replaces the code option) | 90 days | 1 day to 3 years | the release tracker's pruning |
| `auth.deleted_account_retention` (exists) | 30 days | as today | `auth_cleanup` |
| `orgs.deleted_org_retention` (exists, multi-tenant) | 30 days | as today | `orgs_purge` |

- `GET /ops/retention` is a read-only summary: each kind of data, its setting, current value, oldest row, and the enforcing job's last run. Changes go through `PUT /ops/settings/{key}`, so there is one write path with reasons, versions, history and audit events (amends ADR-0026's `PUT /ops/retention`).
- The new `retention` job (daily, `internal/jobs/retention`, editable in `/ops/jobs`) deletes in batches of 5,000 rows per statement until nothing is older, so it never holds long locks; library `auditpg.Store.DeleteBefore(ctx, before, limit)`, `settings.Store.DeleteHistoryBefore`, `jobs.Manager.DeleteHistoryBefore`.
- It records one `retention.purged` audit event per kind with the count and cutoff, so shortened retention is visible even after the rows are gone.
- River's finished-job retention stays a code option: River applies it at client start, not live.
- Permission: `ops.settings.read` for the summary.

### 5. Maintenance mode

- Settings `maintenance.enabled` (bool, default false), `maintenance.message` (string, at most 500 characters) and `maintenance.retry_after` (duration, default 5 minutes). They reach every instance within a second (LISTEN/NOTIFY) and turning it on records an audit event like any setting change.
- While on, a middleware in the app answers **503** with problem code `maintenance`, the message and a `Retry-After` header.
- Still served: `/livez` and `/readyz` (so load balancers keep instances in rotation), `/docs` and `/openapi.json`, `/ops/*`, and `/v1/auth/*` (so staff can sign in and turn it off).
- Break-glass: `go run ./cmd/api maintenance on|off [--message …]` writes the setting directly, for when nobody can reach `/ops`.
- The middleware reads the setting from memory; it adds no database query per request.

### 6. Postman collection and `llms.txt`

- `go run ./cmd/api openapi` gains `--dir api`, writing `openapi.json`, `postman_collection.json` and `llms.txt` together; without `--dir` it prints the spec as today.
- Library `modules/openapi/export`: `Postman(spec)` builds a Postman v2.1 collection (a folder per tag, `{{baseUrl}}` and `{{token}}` variables, bearer auth, example bodies from the schema's examples); `LLMs(spec)` writes [llms.txt](https://llmstxt.org) Markdown (the API's title and description, how to authenticate, each tag's operations as `METHOD path: summary`, links to `/openapi.json` and `/docs`). Both are deterministic, so the committed files diff cleanly.
- `TestOpenAPIUpToDate` checks all three files; `orb dev` and `orb upgrade` regenerate all three (ADR-0050's derived paths gain the two new files).
- These describe the generated app's API. The framework's own `llms.txt` on `docs.gorbital.dev` is ADR-0049's.

### 7. `orb doctor`

Read-only; prints each check as `ok`, `warn` or `fail` with the fix; exit 1 on any `fail`; `--json`.

| Check | Fails when | Warns when |
|---|---|---|
| Toolchain | Go older than `go.mod`'s `go` line; git missing | Docker missing or not running (Full preset) |
| Project | No `go.mod`, `gorbital.yaml` or readable `gorbital.lock` | Lock written by a newer `orb`; tracked files edited (count only) |
| Versions | A `replace` points at a missing checkout | The app's library version or lock release is older than this `orb` (suggests `orb upgrade`) |
| Anchors and blocks | `//orb:anchor modules` or the `.env.example` mail block is missing (generators would stop) | |
| Environment | `.env` holds a secret but isn't ignored by git | `.env` lacks keys `.env.example` has; `AUTH_ENCRYPTION_KEYS` malformed. Values are never printed |
| Exports | | `api/openapi.json`, `postman_collection.json` or `llms.txt` stale |
| Database (Full preset, when `DATABASE_URL` is reachable) | Migration files older than the database's version are missing | Pending migrations |
| Ports | | The API, PostgreSQL or Mailpit port is taken by another process |

- The CLI stays free of database drivers: the database check runs the app's `go run ./cmd/migrate --status --json` (new flag, built on `postgres.Migrations`).
- Each check has a timeout; an unreachable database is a `warn` with the address to check, not a hang.

### 8. Permissions and 2FA

- New permissions `ops.system.read`; existing ones cover the rest. `platform_admin` gets every `ops.*` permission, `ops_viewer` every read.
- Every new endpoint goes through `authorize`, so it requires a platform role and a session with 2FA, as ADR-0043 made true for all of `/ops`. A table test calls every `/ops` operation in the OpenAPI document without a session, without the permission, and without 2FA, and expects 401, 403 and 403 `mfa_required`, so a future endpoint that forgets can't pass.

### 9. How existing apps receive it

- Library changes (`auditpg`, `jobs`, `settings`, `releases`, `openapi/export`): `go get`.
- App changes (ops module, `retention` job, maintenance middleware and command, settings, exports): template changes to both golden apps, delivered by `orb upgrade`, the first release to exercise it on real template changes. No new migration.
- `releases.WithRetention` stays as a deprecated option that sets the setting's default, removed after v1.0.

### 10. Order

1. Permission table test, `ops.system.read`, `GET /ops/system`.
2. Audit stats and jobs overview.
3. Retention settings, `retention` job, `GET /ops/retention`.
4. Maintenance mode and the break-glass command.
5. API exports.
6. `orb doctor`.
7. Upgrade note, docs, threat model rows, and the v0.5 definition of done checked end to end.

## Why

- Settings already give retention and maintenance mode bounds, reasons, history, audit and live propagation; a second configuration store would duplicate all of it.
- One write path for retention means shortening audit retention is itself audited, and the purge leaves a summary event.
- Maintenance mode that keeps health checks, sign-in and `/ops` open can always be turned off, and doesn't make load balancers drop every instance.
- Exports generated by the app, from its own spec, keep generated apps independent of `orb` (architecture principle 8).
- `orb doctor` calling the app for database state keeps the CLI small and uses the app's own migration files.

## Trade-offs

- `GET /ops/system` shows one instance per request; a cluster view is v1.1's live observability.
- Deleting audit events at all is a policy choice; teams with longer obligations raise the setting (up to 10 years) or export events elsewhere.
- Maintenance mode leaves `/v1/auth/*` open to everyone, not only staff: sign-in works, but everything after it answers 503.
- `orb doctor` runs `go run` for exports and database checks, which takes seconds on a cold build cache.

## Consequences

- ADR-0026: `PUT /ops/retention` becomes settings writes plus a read-only summary; `GET /ops/jobs/overview` added; maintenance mode's exempt routes and break-glass command defined.
- ADR-0027: Postman and `llms.txt` export defined.
- ADR-0015: new permissions, setting keys, problem code `maintenance` and `orb doctor` output are public API.
- Threat model: rows for erasing audit trails through retention, maintenance mode lockout or abuse, and information disclosure from `/ops/system` and `orb doctor`.

## Implementation notes

| Step | State | Notes |
|---|---|---|
| 1. Permission table test, `GET /ops/system` | Done (2026-09-15) | `ops.system.read` for `platform_admin` and `ops_viewer`; `GET /ops/system` from `internal/app/system.go` (`releases.Tracker.InstanceID`, `health.Checker.Check`, `pgxpool.Stat`, `postgres.Migrations`, `runtime.ReadMemStats`), with 2-second bounds on its database calls and fixed error descriptions. The permission test runs at the use-case layer instead of over HTTP, where input validation answers 422 before any permission check: `TestEveryOperationAuthorizesFirst` calls every exported ops `Service` method by reflection without a session, without permissions and needing 2FA, on a service with no dependencies, so a method that skips `authorize` panics and fails; `TestOpsOperationsDeclareSecurity` checks that every `/ops` operation in the spec declares bearer security and 401 and 403 responses. `TestOpsSystem` checks the report and that it holds no connection string. Both golden apps |
| 2. Audit stats, jobs overview | Done (2026-09-15) | `auditpg.Store.Stats`: the list's filter conditions (now shared through `Filter.conditions`), one `GROUP BY` with `sum(n) OVER ()` for the total before `LIMIT`, a 5-second context timeout; `group_by=day` returns every day instead of the top 50. `jobs.Manager.Overview`: one grouped count over `river_job` for available, scheduled, running and retryable jobs and those discarded in the last 24 hours, merged with active queues (paused flag), plus definitions whose last run is retryable or discarded; a 5-second timeout. `GET /ops/audit/stats` (`ops.audit.read`, 422 `invalid_audit_filter`) and `GET /ops/jobs/overview` (`ops.jobs.read`) in both golden apps. Tests: `TestStats`, `TestStatsCountsGroupsBeyondTheLimit`, `TestOverview` (scheduled and discarded jobs, pausing), `TestOpsAuditStatsAndJobsOverview` |
| 3. Retention | Done (2026-09-15) | Library: `auditpg.Store.DeleteBefore`/`Oldest`, `settings.Store.DeleteHistoryBefore`/`OldestHistory`, `jobs.Manager.DeleteHistoryBefore`/`OldestHistory` (batched `DELETE … WHERE id IN (SELECT … LIMIT n)`), and `releases.WithRetentionFunc` (read at each start, clamped to 1 day–3 years) instead of deprecating `WithRetention`, which stays for apps without settings. App: settings `audit.retention` and `ops.history_retention` (365 days, 30 days–10 years, reason required) and `releases.instance_retention` (90 days) in group `retention`; `internal/jobs/retention` (daily at 04:15, 30-minute timeout) deletes targets in batches of 5,000, continues past a failing target, and records `retention.purged` with the row count and cutoff under the `system:retention` actor; `GET /ops/retention` (`ops.settings.read`) lists each kind of data with its setting, live value, oldest row when known, and enforcing job's last and next run: audit events, settings history, job configuration history, release instances, deleted accounts, and deleted organisations in multi-tenant apps. Oldest rows are reported for the data the retention job deletes. Tests: `TestDeleteBeforeAndOldest` (auditpg), `TestDeleteHistoryBeforeAndOldest` (settings, jobs), `TestRetentionFuncAppliesAtStart`, `TestWorkerDeletesInBatchesAndRecords`, `TestWorkerContinuesPastAFailingTarget`, `TestOpsRetention` |
| 4. Maintenance mode | Done (2026-09-15) | Settings `maintenance.enabled` (reason required), `maintenance.message` (at most 500 characters) and `maintenance.retry_after` (5 minutes, 1 minute–24 hours) in group `maintenance`. `internal/app/maintenance.go`: a middleware after the body limit and before authentication, reading the settings from memory, answers 503 `maintenance` with the message and `Retry-After`; open routes are `/livez`, `/readyz`, `/version`, `/openapi.json`, `/docs`, `/.well-known/*`, `/ops/*` and `/v1/auth/*`. `go run ./cmd/api maintenance on|off [--message …]` calls `app.SetMaintenance`, which writes the settings directly as `system:cli`. Tests: `TestMaintenanceMode` (on through `/ops/settings`, reason enforced, product route 503 with message and `Retry-After: 300`, every open route served, sign-in not blocked, off again), `TestMaintenanceCommand` (an instance started after the command is in maintenance with its message, and serves again after `off`) |
| 5. API exports | Done (2026-09-15) | `Postman` and `LLMs` live in `modules/openapi/reference`, not a new `export` package: the reference already parses the document, groups operations by tag, builds examples from schemas and serves each page as Markdown, so the exports reuse all of it. `Postman`: a v2.1 collection with a folder per tag, `{{baseUrl}}` (default `http://localhost:8080`) and `{{token}}`, bearer auth on operations with security, path variables as `:name`, query parameters (optional ones disabled) with example values, and example JSON bodies. `LLMs`: title, summary, how to authenticate, and every endpoint by tag linked to its Markdown page under `/docs`. Both deterministic. Apps: `app.WriteAPIFiles` (`internal/app/api_files.go`, identical in all three golden apps) behind `go run ./cmd/api openapi --dir api`; `TestOpenAPIUpToDate` checks all three files; `orb upgrade` and `orb add orgs` regenerate them (derived paths); `orb gen resource`'s next steps and every guide and golden README use `--dir api`. Not done: `orb dev` doesn't refresh the files, which ADR-0027 planned; the up-to-date test catches stale files instead. Tests: `TestPostman`, `TestLLMs`, `TestOpenAPIUpToDate` (all golden apps) |
| 6. `orb doctor` | Done (2026-09-15) | `cli/internal/cli/doctor.go`: checks `go` (against `go.mod`'s go directive; pre-releases count as older), `git`, `docker` (Full), `gorbital.yaml`, `gorbital.lock` (version, v1, release against this `orb`, edited tracked files by hash), `library` (`replace` pointing at a checkout), `anchor` (`//orb:anchor modules`, `jobs`, `org-permissions` in multi-tenant apps, the mail block), `.env` (missing variables by name, secrets not ignored by git; values never printed), `api files` (exported to a temporary directory with `go run ./cmd/api openapi --dir` and compared), and `configuration`/`database` through the app's new `go run ./cmd/migrate --status --json` (`app.WriteMigrationStatus`: configuration error, database error, current, latest, pending). Configuration problems such as a malformed `AUTH_ENCRYPTION_KEYS` come from the app's own `LoadConfig` there, not from a second copy of its validation in the CLI. `--fast` skips the checks that build the app; exit 1 when any check fails. Not done: the port check, since a taken port is usually the app's own running services and `orb dev` already reports ports it can't use. Tests: `TestDoctorOnANewApp`, `TestDoctorFindsProblems` (anchor, secret `.env` not ignored and never printed, stale `llms.txt`, pending, configuration error, database ahead of the code), `TestDoctorOnAMinimalApp`, `TestVersionAtLeast`, `TestMigrationStatus` (both Full golden apps) |
| 7. Upgrade note, docs, threat model, done-when | Done (2026-09-15) | [Upgrade notes](../guides/upgrade-notes.md) for v0.5, rendered on the docs site, lead with the one behaviour change existing apps must act on before deploying: audit events older than 365 days are deleted. Threat model rows 29–31 added and done, row 18 checked by the authorisation tests. Roadmap v0.5 marked done with results; architecture, README and site updated. `docs/guides/ops-api.md` documents every new endpoint, setting, error code and command, and renders on the site |

## Security review fixes (2026-09-16)

The internal security review of September 2026 (findings OPS-2 to OPS-10) changed several v0.5 operations; the library side is recorded in ADR-0033, ADR-0036 and ADR-0037.

| Finding | Change in both golden apps |
|---|---|
| OPS-2, OPS-3 | `/ops/queues/{name}/pause` takes `{reason}` (required), resume an optional one; run-now answers 429 `job_run_limited`, retry 409 `job_not_retryable`; `job_reason_required` covers timeout, attempts and queue changes |
| OPS-6 | 503 `audit_query_timeout` when an audit listing or stats query runs over 5 seconds |
| OPS-7 | Ops changes over HTTP record the client's IP address and user agent, through `auth.Middleware` and `audit.FromContext` |
| OPS-8 | `POST /ops/mail/test` authorises fully before queueing and allows 5 test emails an hour per operator |
| OPS-9 | Reason required for `mail.from_name`, `mail.from_email`, `mail.reply_to`, `auth.verification_code_ttl`, and in multi-tenant apps `orgs.invitation_url` and `orgs.invitation_ttl`. Reviewed and left without: `example.ping_message`, `maintenance.message`, `maintenance.retry_after` and `releases.instance_retention`, which change neither access, lifetimes of secrets nor where emails and links go |
| OPS-10 | `settings.StringList` without `MaxItems` is limited to `settings.DefaultMaxItems` (100). Accepted: `/ops/releases` returns instance host names to `ops.releases.read`; they identify pods for operators, while threat 31's promise covers `/ops/system`'s infrastructure details (connection strings, environment). Accepted: when an audit write fails after a settings or job change, the history table still records it, and run-now, retry, cancel, pause and resume have only the logged error; making the audit write part of the change would let an audit outage block incident response |

New error codes `job_run_limited`, `job_not_retryable` and `audit_query_timeout` are recorded in `api/surface.json`; `TestOpsAPICompatible` passes against the unchanged baseline.

## Maintainer's answers (2026-09-15)

1. Retention is changed through `/ops/settings`, with a read-only `GET /ops/retention` summary (section 4).
2. Audit events are kept 365 days by default, at least 30 days and at most 10 years.
3. Maintenance mode keeps `/v1/auth/*`, `/ops/*`, health checks and docs open, answers 503 elsewhere, and has a `maintenance off` command (section 5); no read-only mode.
4. `orb doctor` checks the database through the app's `cmd/migrate --status` (section 7).
