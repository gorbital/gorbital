# ADR-0040: Release tracking

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0026

## Context

ADR-0026 plans a release monitor: each instance records its version, commit, build time and start time at boot, with no CI webhook, and `GET /ops/releases` and `GET /ops/releases/current` show them. The roadmap puts `modules/releases` in v0.2 and the ops endpoints in v0.5, which would leave a module with nothing reading it for three milestones.

Operators ask release questions during incidents and deploys: which version is running, since when, on how many instances, is a rolling deploy finished, and did an instance start a build with uncommitted changes. Core `buildinfo` already reads version, commit, build time, the modified flag and the Go version from the binary.

Questions to settle:

- What a record is: a release, an instance start, or both.
- How "current" is known when instances stop without a clean shutdown.
- Whether the ops endpoints move to v0.2.
- Whether the code is a library module or app-owned.

## Options for "current"

1. The newest recorded start. Wrong during rolling deploys and after rollbacks.
2. Instances mark themselves stopped on shutdown. Crashed or killed instances stay "running" forever.
3. Instances send a heartbeat; an instance is running while its heartbeat is recent, and marks itself stopped on a clean shutdown.

Option 3.

## Decision

`apistock.dev/modules/releases` records one row per instance start and derives releases from them.

### Library or app-owned

A library module, like `settings`, `jobs` and `auditpg`: release tracking is operational infrastructure with no business rules, it must behave the same in every app, and fixes should arrive with `go get`. The app owns what it shows: the `/ops/releases` endpoints live in the app's `ops` module (ADR-0019) and call the module's query API.

### Table

| Topic | Decision |
|---|---|
| Table | `release_instances`: `id` (identity), `instance_id` (random, per process), `version`, `commit`, `build_time`, `modified`, `go_version`, `host`, `started_at`, `last_seen_at`, `stopped_at` (nullable) |
| Bounds | Version 100, commit 64, host 255 characters; build time stored as `timestamptz` when it parses, otherwise NULL |
| Indexes | `(version, commit)`; `(last_seen_at)` for running instances and pruning |
| Tenancy | Global, no `org_id`: releases belong to the deployment, not to organisations |
| Migrations | `releases.Migrations`, copied into the app's `db/migrations` (ADR-0005) |

### Recording

| Topic | Decision |
|---|---|
| API | `releases.NewTracker(pool, buildinfo.Read(), opts...)` returns a `Tracker`, an `app.Runner` (ADR-0017) started with the other workers |
| Start | `Run` inserts the instance row, then prunes stopped instances older than the retention |
| Heartbeat | Every 30 seconds (`WithHeartbeat`, 5 s to 5 min), `last_seen_at` is updated |
| Stop | When `Run`'s context ends, `stopped_at` is set with a short timeout, so graceful shutdowns show immediately |
| Running | An instance is running when `stopped_at` is NULL and `last_seen_at` is within 3 heartbeats |
| Failures | A failed insert or heartbeat is logged and retried at the next heartbeat; release tracking never stops the app from starting or serving |
| Host | `os.Hostname()` by default, or `WithHost` (for example a pod name); no IP addresses |
| Retention | Instances last seen more than 90 days ago, stopped or crashed, are deleted when a tracker starts (`WithRetention`, 1 day to 3 years). Releases stay listed while any of their instances remain. Configurable retention policies in `/ops/retention` stay in v0.5 |
| Lost rows | If a heartbeat finds the instance's row gone, the tracker records the instance again |
| Tests | `WithClock` for time; tests against Docker PostgreSQL with `pgtest` |

### Queries

| Topic | Decision |
|---|---|
| `Releases(ctx, filter)` | One entry per (version, commit): first started, last seen, instances running now, total starts, and whether any build was modified. Newest first by first start; cursor pagination |
| `Current(ctx)` | Every release with a running instance, with its running instances. More than one during a rolling deploy; none if every heartbeat is stale |
| `Instances(ctx, filter)` | Instance starts, newest first, filtered by version and commit or by running; cursor pagination |

### Ops APIs (moved from v0.5 to v0.2)

| Endpoint | Result |
|---|---|
| `GET /ops/releases` | Releases, newest first |
| `GET /ops/releases/current` | Releases running now, with their instances |
| `GET /ops/releases/instances` | Instance starts, with `version`, `commit` and `running` filters |

Permission `ops.releases.read`, granted to `platform_admin` and `ops_viewer`. Required 2FA on ops routes remains v0.3 (ADR-0038).

### Not in v0.2

Deployment webhooks (v1.1), release notes or changelogs, marking a release good or bad, alerting on version drift, configurable retention policies (v0.5).

## Why

- A heartbeat is the only signal that stays correct when instances crash, are killed or lose their network; clean shutdowns still show at once.
- Recording from the binary itself needs no CI integration and works on any host.
- Moving the endpoints forward gives the module a consumer now and answers a common incident question without extra tooling.

## Trade-offs

- One small write per instance every 30 seconds.
- An instance that loses its database connection looks stopped after 90 seconds while still serving.
- Versions come from build info: builds without VCS stamping or a link-time version show `dev` and no commit, which the `/ops` responses make visible rather than hide.
- Host names can reveal infrastructure naming to ops users; they are behind `ops.releases.read`.

## Consequences

- `examples/full-single` runs the tracker as a worker and serves the three endpoints.
- Permission `ops.releases.read`, the endpoints' response fields and the `release_instances` identity column are public API (ADR-0015).
- ADR-0026's release monitor is delivered in v0.2; the roadmap's v0.5 list drops it.
