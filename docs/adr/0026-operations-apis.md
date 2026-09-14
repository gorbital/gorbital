# ADR-0026: Operations APIs

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0010 · **Amended by:** ADR-0031

## Context

Production apps need operational visibility: who did what, whether the system is healthy, which release is running, whether jobs are failing, and how long data is kept. The maintainer's previous production API included audit logs, error logs, system metrics, deployment tracking and a settings center. apistock should provide these by default without requiring a dashboard product, and without turning the application database into a log store.

## Options

1. No ops features; point users to external tools.
2. A built-in admin web UI.
3. Permission-protected `/ops/*` APIs in the generated app, phased by risk; UI left to teams.

## Decision

Option 3.

### v1 (Full preset)

| Feature | Endpoints | Notes |
|---|---|---|
| Audit logs | `GET /ops/audit`, `GET /ops/audit/{id}`, `GET /ops/audit/stats` | Filters: actor, action, resource, org, outcome, time range; cursor pagination |
| System health | `GET /ops/system` | Database status and pool statistics, migration version, Go runtime, uptime, dependency checks |
| Release monitor | `GET /ops/releases`, `GET /ops/releases/current` | Each instance records version, commit, build time and start time at boot; no CI webhook required |
| Jobs overview | `GET /ops/jobs`, `GET /ops/jobs/{id}` | Queues, failed and retrying jobs from River |
| Retention | `GET/PUT /ops/retention` | Policies for audit events, sessions, releases, deleted accounts; enforced by cron jobs |
| Maintenance mode | Runtime setting (ADR-0031) | Returns 503 with a message for non-ops routes |
| Runtime settings | `GET /ops/settings`, `GET/PUT/DELETE /ops/settings/{key}`, `GET /ops/settings/{key}/history` | Moved from v1.1 to v0.2 by ADR-0031 |

### v1.1

Feature flags, live observability (per-instance in-memory recent logs and metrics over SSE), incident reports (grouped errors with triage status plus incidents with timeline), API keys, Prometheus `/metrics` option, optional GitHub deployment webhook.

### Rules

- `/ops/*` requires platform roles (`ops.*` permissions) and enrolled 2FA.
- Ops responses contain no secrets, tokens, or unnecessary personal data.
- **Not built:** application logs stored in PostgreSQL; host CPU/disk metrics presented as app metrics; source code snippets in error responses.
- The generated `ops` module composes query APIs from `auditpg`, `jobs`, `releases`, `settings` and `postgres` (ADR-0019); the library modules don't depend on each other.
- Ops routes can be served on a separate internal port by configuration.
- No admin web UI in v1.

## Why

Teams get operational visibility on day one through documented APIs, while long-term telemetry stays in dedicated observability tools.

## Trade-offs

- Teams must build or choose a UI.
- Per-instance live data (v1.1) isn't aggregated across instances.

## Consequences

Ops endpoints appear in the OpenAPI spec and Scalar docs under a separate tag.
