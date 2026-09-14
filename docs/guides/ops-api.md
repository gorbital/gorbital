# Ops API reference

Admin APIs of the Full preset (`internal/modules/ops`), implemented in `examples/full-single`. The full schema is in the app's `api/openapi.json` and at `/docs`. Decisions: [ADR-0026](../adr/0026-operations-apis.md), [ADR-0031](../adr/0031-runtime-settings.md), [ADR-0033](../adr/0033-background-jobs.md), [ADR-0034](../adr/0034-interim-ops-token.md).

## Authentication

Until authentication ships, every `/ops/*` request needs the ops token:

```bash
export OPS_TOKEN=$(openssl rand -hex 32)   # set the same value in the app's environment
curl -H "Authorization: Bearer $OPS_TOKEN" http://127.0.0.1:8080/ops/settings
```

| Situation | Response |
|---|---|
| `OPS_TOKEN` not configured | 404 `not_found` for every `/ops/*` path |
| Missing or wrong token | 401 `unauthenticated`, `WWW-Authenticate: Bearer realm="ops"` |
| Valid token | Acts as actor `ops-token` with every ops permission |

## Permissions

| Permission | Allows |
|---|---|
| `ops.settings.read` | List and read settings and their history |
| `ops.settings.write` | Change and reset settings |
| `ops.jobs.read` | Read job definitions, scheduled jobs, runs and queues |
| `ops.jobs.write` | Change and reset job configuration; pause and resume queues |
| `ops.jobs.run` | Run a job now, retry or cancel a run |

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

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/example.ping_message \
  -H "Authorization: Bearer $OPS_TOKEN" -H 'Content-Type: application/json' \
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
  "updated_by": "ops-token",
  "reason_required": false,
  "restart_required": false,
  "restart_pending": false,
  "constraints": {"max_len": 100}
}
```

## Job definitions

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/jobs/definitions` | All definitions with effective and default config, next and last run | 200 `{definitions: [...]}` |
| `GET /ops/jobs/scheduled` | Enabled scheduled jobs, soonest first | 200 `{definitions: [...]}` |
| `GET /ops/jobs/definitions/{name}` | One definition | 200 |
| `PUT /ops/jobs/definitions/{name}` | Change only the sent fields: `{enabled?, schedule?, timeout?, max_attempts?, queue?, priority?, version, reason?}` | 200 |
| `DELETE /ops/jobs/definitions/{name}` | Reset to code defaults: `{version, reason?}` | 200 |
| `GET /ops/jobs/definitions/{name}/history?before=&limit=` | Changes: `action`, `old_config`, `new_config`, reason, actor | 200 `{changes: [...]}` |
| `POST /ops/jobs/definitions/{name}/run` | Run now | 202 with the run |

```bash
curl -X PUT http://127.0.0.1:8080/ops/jobs/definitions/heartbeat \
  -H "Authorization: Bearer $OPS_TOKEN" -H 'Content-Type: application/json' \
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
  "updated_by": "ops-token",
  "next_run_at": "2026-09-14T14:00:00Z",
  "last_run": {"id": 42, "kind": "heartbeat", "state": "completed", "attempt": 1, "…": "…"}
}
```

A reason is required to disable a job or change an enabled job's schedule.

## Job runs

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/jobs/runs?kind=&queue=&state=&limit=&cursor=` | Runs, newest first; `state` is comma-separated: available, cancelled, completed, discarded, pending, retryable, running, scheduled | 200 `{jobs: [...], next_cursor?}` |
| `GET /ops/jobs/runs/{id}` | One run with attempt errors | 200 |
| `POST /ops/jobs/runs/{id}/retry` | Make it run again now | 200 |
| `POST /ops/jobs/runs/{id}/cancel` | Cancel; a running job's context is cancelled | 200 |

Run fields: `id`, `kind`, `queue`, `state`, `attempt`, `max_attempts`, `priority`, `created_at`, `scheduled_at`, `attempted_at`, `finalized_at`, `errors[{at, attempt, message}]`, `request_id`, `actor_kind`, `actor_id`. Arguments are never returned.

## Queues

| Method and path | Purpose | Success |
|---|---|---|
| `GET /ops/queues` | Active queues: `name`, `paused`, `paused_at`, `created_at`, `updated_at` | 200 `{queues: [...]}` |
| `POST /ops/queues/{name}/pause` | Stop every instance fetching from the queue | 204 |
| `POST /ops/queues/{name}/resume` | Resume it | 204 |

## Error codes

| Code | Status | When |
|---|---|---|
| `unauthenticated` | 401 | Missing or invalid ops token |
| `forbidden` | 403 | Missing permission |
| `not_found` | 404 | Ops disabled, or no such route |
| `validation_failed` | 422 | Request doesn't match the schema |
| `setting_not_found` | 404 | Unknown setting key |
| `setting_version_conflict` | 409 | Setting changed since it was read |
| `setting_reason_required` | 422 | Reason missing for a setting that requires one |
| `invalid_setting_value` | 422 | Value fails the setting's type or validation; `detail` says why |
| `job_definition_not_found` | 404 | Unknown job name |
| `job_definition_version_conflict` | 409 | Definition changed since it was read |
| `job_reason_required` | 422 | Reason missing to disable or reschedule |
| `invalid_job_config` | 422 | Schedule, timeout, attempts or priority out of bounds; `detail` says why |
| `job_definition_disabled` | 409 | Run now on a disabled job |
| `job_not_found` | 404 | Unknown run ID (or removed by retention) |
| `queue_not_active` | 422 | No worker runs that queue |
| `invalid_cursor` | 400 | Malformed `cursor` |
| `invalid_job_state` | 422 | Unknown `state` filter |

Error codes are public API: new ones are added, existing ones never change.

## Audit actions

| Action | Resource |
|---|---|
| `settings.value.changed` | `setting` |
| `jobs.definition.changed` | `job_definition` |
| `jobs.definition.run_requested` | `job_definition` |
| `jobs.run.retried` | `job` |
| `jobs.run.cancelled` | `job` |
| `jobs.queue.paused` | `job_queue` |
| `jobs.queue.resumed` | `job_queue` |

Until an audit store ships, `examples/full-single` records these events in its log.
