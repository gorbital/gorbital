# ADR-0073: The Dev Portal's Observability screen

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0028, ADR-0064, ADR-0066, ADR-0067

## Context

Phase 8 of the [Dev Portal roadmap](../dev-portal-roadmap.md) is the Observability screen: service health, the API's rates and percentiles per route, the database's connections, cache, sizes and locks, query performance from `pg_stat_statements` with `EXPLAIN` and index suggestions, the machine and the Go runtime, and jobs and sign-ins. The roadmap sketched an OTLP receiver inside `orb dev` with a local store, replacing `orb dev --observability` (Grafana) for local viewing.

What exists: `/ops/observability/overview` and `/routes` answer request rate, error rate and p50, p95 and p99 per route from the app's request minutes ([ADR-0064](0064-observability.md)); `/ops/system` answers the readiness checks, the pool's counters, the migration state and the Go runtime; `/ops/jobs/overview` the jobs; the audit statistics the sign-ins by method. The log store ([ADR-0072](0072-local-log-store.md)) has every record. What is missing is the database's own statistics, the machine, a health table across services, and the query performance view.

## Options

### An OTLP receiver in `orb dev`

| Option | Verdict |
|---|---|
| Receive the app's traces and metrics over OTLP and store them under `.orb` | Rejected for now: the request minutes already answer the API questions the screen asks, the log store answers "what happened", and traces are what Grafana with Tempo (`orb dev --observability`) shows well; a receiver and a trace store in `orb` would be a second observability stack to keep. The item stays on the roadmap as a later phase |
| **Build the screen on what the app and the database already know, and add what only `orb dev` can see: the machine, the app process and the services** | **Chosen** |

### The database's statistics

| Option | Verdict |
|---|---|
| Through the app's `/ops/system` | Rejected: the app's report is deliberately small (no host names, no query texts); the operators' API isn't the place for `pg_stat_activity` |
| **`pgmeta` reads `pg_stat_database`, `pg_stat_activity`, `pg_locks`, `pg_stat_user_tables`, the relation sizes and `pg_stat_statements` with the portal's own connection (read-only transactions); `orb dev` serves them under `/_portal/api/db/`** | **Chosen**: the same path as the Table Editor, development only |

### Query performance

`pg_stat_statements` needs `shared_preload_libraries`, which is a server setting: the development `compose.yaml` gains `command: ["postgres", "-c", "shared_preload_libraries=pg_stat_statements"]`, and `pgmeta` creates the extension on first use when the server preloads it. Without the preload, the endpoint answers `available: false` with the line to add; nothing fails. `EXPLAIN` of a normalised statement uses the SQL editor's explain endpoint (`GENERIC_PLAN` where the placeholders remain). Index suggestions come from the catalog and the statistics (foreign keys without an index, indexes never scanned, tables read by sequential scans, tables waiting for a vacuum), each with the SQL to run and the reason with its numbers; they are suggestions, not decisions.

### The machine

| Option | Verdict |
|---|---|
| Read `/proc` and `sysctl` by hand | Rejected: two operating systems, many corner cases |
| **`github.com/shirou/gopsutil/v4` (pure Go on macOS and Linux) sampled every 2 seconds by `orb dev`: the host's CPU, load, memory and the app directory's volume; the app process and `orb` (CPU, resident memory, threads, open files)** | **Chosen**: a request never waits for a measurement, and the app process's numbers come from outside the process |

## Decision

| Piece | Decision |
|---|---|
| `pgmeta` | `Stats` (database, version, size, connections against `max_connections` by application and state, cache and index hit ratios, commits, rollbacks, deadlocks, temp bytes, the 50 largest tables with scans, dead rows and last vacuum, lock waits with the blocking PIDs, statements running for over a second), `Statements` (sort by total time, mean time, calls, rows, max time; `available` and `reason`), `ResetStatements`, `Advise` |
| Portal | `GET /_portal/api/health` (app readiness, PostgreSQL with connections and locks, Mailpit, the other Compose services from `docker compose ps`), `GET /_portal/api/system` (the sampler's last sample), `GET /_portal/api/db/stats`, `GET /_portal/api/db/statements?sort=&limit=`, `POST /_portal/api/db/statements/reset`, `GET /_portal/api/db/advice` |
| Compose | `pg_stat_statements` preloaded in the template and the golden apps |
| The screen | Health table; API from `/ops/observability`; Database from `db/stats` and `/ops/system`'s pool; Queries from `db/statements` with Explain and Reset; Advice; System from `system` and `/ops/system`'s runtime; Jobs and auth from `/ops/jobs/overview` and `/ops/audit/stats` |
| Also in this phase | `email_taken` (409) and `unknown_role` (422) on the operators' account APIs, which answered 500 (found by the Authentication screen) |

## Why

- Everything the screen shows is either what the app already reports to operators or what only the developer's machine can see; nothing new runs in production.
- `pg_stat_statements` is the standard answer to "which query costs the most"; preloading it in development costs nothing.
- Suggestions carry their numbers and their SQL, so the developer can judge them and act in the SQL editor.

## Trade-offs

- Existing apps must recreate their PostgreSQL container after `orb upgrade` adds the preload (`docker compose up -d --force-recreate postgres`); until then the Queries view says so.
- Statement texts are normalised by PostgreSQL (`$1` placeholders), so `EXPLAIN` gives generic plans.
- The sampler's numbers are the machine's, not a container's: with the app in Docker they would be the host's.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)): `db/statements` shows query texts and `db/stats` the connected applications, behind the portal's guard, development only.
- Upgrade notes: the compose change and the two error codes.
- Guides: [observability](../guides/observability.md) ("The Observability screen"), [Dev Portal](../guides/dev-portal.md), [local development](../guides/local-development.md).

## Implementation notes (2026-09-16)

`TestStatsStatementsAndAdvice` (`cli/internal/pgmeta`, against the test database, with and without the extension), `TestObservabilityEndpoints` (`cli/internal/portal`), `TestOpsUsers` (the two error codes) in both Full apps.
