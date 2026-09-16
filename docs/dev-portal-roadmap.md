# Dev Portal roadmap

**Status:** In progress (phase 0 done 2026-09-16) · **Decision record:** [ADR-0066](adr/0066-dev-portal.md) · **Builds on:** [ADR-0065](adr/0065-local-dev-console-apis.md), [ADR-0028](adr/0028-local-development-environment.md)

The Dev Portal is the local development UI that `orb dev` serves at `http://127.0.0.1:3100`. It shows what the running app is doing (routes, requests, logs, jobs, mail, database) and lets the developer change the project (tables, migrations, jobs, configuration) from the browser. The UI is built in `gorbital-dashboards/apps/devtools` (a Next.js static export), copied into the `orb` binary by `scripts/sync-portal.sh`, and backed by a portal server inside `orb dev` (`cli/internal/portal`), the app's development-only `/_dev/*` APIs ([ADR-0065](adr/0065-local-dev-console-apis.md)) and the app's `/ops/*` APIs ([ADR-0026](adr/0026-operations-apis.md)). Three rules hold for every phase:

| Rule | What it means |
|---|---|
| **Code stays the source of truth** | Every schema or code change the UI makes is a file in git: a migration under `db/migrations`, a generated Go file, an edited `.env`. The portal keeps no state of its own that the code doesn't already hold; its local database (from Phase 3) stores only query history, logs and metrics, and can be deleted at any time |
| **The portal and the CLI share one engine** | A portal button runs the same generator library as `orb gen …`, through the same flow: plan, diff preview, apply. Nothing is possible in the portal that isn't possible from the terminal, and the two can't drift |
| **Local only** | The portal binds to 127.0.0.1, needs a per-run token, refuses to start when `APP_ENV` is production, and ships embedded in the `orb` binary so that users need no Node.js. A page on another website can't read or drive it (the same Host, loopback-peer and token checks as the dev console) |

Each phase lives on its own branch, `dev-portal/phase-N`, in both repositories (gorbital and gorbital-dashboards), is built in the order backend → frontend → tests → docs, and is pushed when done. Items are numbered continuously (1–90) so that commits, tests and notes can name them.

## Where things stand

| Phase | Name | Status | Branch |
|---|---|---|---|
| [0](#phase-0-foundations) | Foundations | ✅ Done (2026-09-16); items 6 and 7 ship with Phases 2 and 3 | `dev-portal/phase-0` |
| [1](#phase-1-connect-the-existing-screens) | Connect the existing screens | ✅ Done (2026-09-16); Bootstrap became Requests and Logs, Audit is the audit log | `dev-portal/phase-1` |
| [2](#phase-2-table-editor) | Table Editor | 🔨 In progress (2026-09-16): backend done | `dev-portal/phase-2` |
| [3](#phase-3-sql-editor) | SQL Editor | 🔨 In progress (2026-09-16): backend done; history is a JSON Lines file, not SQLite | `dev-portal/phase-3` |
| [4](#phase-4-schema-visualiser-objects-migrations) | Schema visualiser, objects, migrations | 🔨 In progress (2026-09-16): backend done | `dev-portal/phase-4` |
| [5](#phase-5-authentication) | Authentication | 🔨 In progress (2026-09-16): backend done | `dev-portal/phase-5` |
| [6](#phase-6-jobs) | Jobs | 🔨 In progress (2026-09-16): backend done ([ADR-0071](adr/0071-job-kinds-and-ejection.md)) | `dev-portal/phase-6` |
| [7](#phase-7-logs) | Logs | 🔨 In progress (2026-09-16): backend done ([ADR-0072](adr/0072-local-log-store.md)); the store is JSON Lines under `.orb/portal/logs`, not SQLite | `dev-portal/phase-7` |
| [8](#phase-8-observability) | Observability | 🔨 In progress (2026-09-16): backend done ([ADR-0073](adr/0073-observability-screen.md)); no OTLP receiver, the screen builds on the request minutes, `/ops/system`, `pgmeta` statistics and a machine sampler | `dev-portal/phase-8` |
| [9](#phase-9-mail-env-configuration) | Mail, env, configuration | 🔨 In progress (2026-09-16): backend done ([ADR-0074](adr/0074-dev-mail-previews-and-env-editor.md)) | `dev-portal/phase-9` |
| [10](#phase-10-storage) | Storage | 🔨 In progress (2026-09-17): backend done ([ADR-0075](adr/0075-file-storage.md)) | `dev-portal/phase-10` |
| [11](#phase-11-git) | Git | 🔨 In progress (2026-09-17): backend done ([ADR-0076](adr/0076-git-screen.md)) | `dev-portal/phase-11` |
| [12](#phase-12-generators-and-scaffolding) | Generators and scaffolding | Planned | `dev-portal/phase-12` |
| [13](#phase-13-advanced) | Advanced | ⏸ Skipped for now (2026-09-16, by decision): the work stops after Phase 12 | — |

## Architecture

```text
 browser  http://127.0.0.1:3100
    │
    ▼
┌──────────────────────────────────────────────────────────────────────┐
│ orb dev                                                              │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │ portal server (cli/internal/portal)                            │  │
│  │                                                                │  │
│  │  /                 embedded UI (Next.js static export)         │  │
│  │  /_portal/api/*    state, output, events, restart, generators  │  │
│  │  /_portal/app/*    proxy to the app; adds the dev console      │  │
│  │                    token to /_dev/* requests                   │  │
│  └───────┬──────────────────────┬─────────────────────────────────┘  │
│          │ supervisor           │ proxy                              │
│          ▼                      ▼                                    │
│  ┌──────────────┐   ┌───────────────────────────────────────────┐    │
│  │ generators   │   │ the app (go run ./cmd/api)                │    │
│  │ plan → diff  │   │   /_dev/*   ADR-0065, development only    │    │
│  │ → apply      │   │   /ops/*    ADR-0026, dev-operator        │    │
│  │ (same code   │   │   /v1/*     the app's own API             │    │
│  │  as orb gen) │   └───────────────────────────────────────────┘    │
│  └──────┬───────┘                                                    │
│         │ writes                    ┌───────────────────────────┐    │
│         ▼                           │ .orb/portal.db (SQLite,   │    │
│  project files in git               │ from Phase 3): query      │    │
│  db/migrations/*.sql, *.go, .env    │ history, logs, metrics    │    │
│                                     └───────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────┘
        │
        ▼  Docker Compose (ADR-0028): PostgreSQL, Mailpit until Phase 9
```

The UI never holds the dev console token: the proxy adds it. The portal's own token travels as an `HttpOnly`, `SameSite=Strict` cookie set by the `/_portal/auth` link that `orb dev` prints and opens, or as a bearer header for scripts; requests that change anything also need an `X-Orb-Portal` header, which browsers send only after a CORS preflight the portal never answers. Database reads and writes go through the app (`/_dev`, `/ops`) or through `orb dev`'s own connection to the Compose database; the portal never opens a second path to production data.

## Phase 0: Foundations

Everything later phases stand on: the server, the proxy, the process supervisor, the generator engine and the UI primitives. Items 6 and 7 are designed here and ship with the phase that first needs them.

| # | Item | Piece |
|---|---|---|
| 1 | `orb dev` starts the portal server: embedded UI, bound to 127.0.0.1, a per-run session token, refused when `APP_ENV` is production | Backend |
| 2 | `/_portal/api/*` for the portal's own state, and `/_portal/app/*` proxying to the app, adding the dev console token to `/_dev/*` and passing `/ops/*` through | Backend |
| 3 | The `/_dev/*` endpoints ([ADR-0065](adr/0065-local-dev-console-apis.md), already built) consumed by the UI through the proxy | Frontend |
| 4 | Process supervisor: `orb dev` runs the app, restarts it on request (`/_portal/api/app/restart`, `stop`, `start`), captures its output and streams state changes and output lines to the UI | Backend |
| 5 | Generators as a library returning file plans (job, resource, migration): `POST /_portal/api/generators/{name}/plan` returns the files and a diff, `apply` writes them; the CLI's `orb gen` calls the same library | Backend |
| 6 | `pgmeta` package: catalog reads (schemas, tables, columns, constraints, indexes) and DDL rendering into migration files. Designed here; ships with Phase 2 | Backend |
| 7 | Local SQLite store `.orb/portal.db` (gitignored) for the portal's own records. Designed here; ships with Phase 3 | Backend |
| 8 | React Query API client with a mock mode: the public demo and the gorbital-web screenshots run on mock data, so screens can be built before their endpoints exist | Frontend |
| 9 | New `packages/ui` primitives styled with the existing tokens: inputs, dialog, sheet, dropdown, tabs, tooltip, toast, ⌘K command palette, data grid, Monaco editor | Frontend |
| 10 | Button, Segmented and Nav made interactive; Table gains sorting and pagination | Frontend |
| 11 | New navigation: Overview; Database (Table Editor, SQL Editor, Schema, Objects, Migrations); Auth; Storage; Jobs; Mail; API (Routes, Modules, Bootstrap, Audit); Logs; Observability; Env & Settings; Git; Generators; Project Settings. Sections whose phase hasn't shipped show what's coming and link here | Frontend |

Docs: [ADR-0066](adr/0066-dev-portal.md), the [local development guide](guides/local-development.md) banner line for the portal, this file.

## Phase 1: Connect the existing screens

The screens that exist on mock data move to live data. No new backend surface except the development-only operator principal.

| # | Item | Piece |
|---|---|---|
| 12 | Routes live from `/_dev/routes`, with a real request builder: auth presets (anonymous, seeded admin, API key), headers, body, the response, and the SQL spans of that request | Frontend |
| 13 | Modules, Bootstrap and Audit on live `/_dev/app` and `/ops/audit` data | Frontend |
| 14 | Jobs: definitions, runs, run now, retry, cancel, pause and resume queues, on the existing `/ops/jobs` endpoints. Needs a dev-operator principal so that `orb dev` can call `/ops` in development without a stored token; how it is minted and why it never exists in production is in [ADR-0066](adr/0066-dev-portal.md) | Backend, frontend |
| 15 | Settings read and changed through `/ops/settings` ([ADR-0031](adr/0031-runtime-settings.md)) | Frontend |
| 16 | Database overview: migration status from `/_dev/migrations`, pool statistics, table sizes, and a working Apply that runs pending migrations through the supervisor | Backend, frontend |
| 17 | Overview health from `/ops/system`, `/livez` and `/readyz`, with the app's state and last output lines from the supervisor | Frontend |

Docs: the [dev console guide](guides/dev-console.md) gains a "From the portal" section; the [ops API guide](guides/ops-api.md) notes the dev-operator principal.

## Phase 2: Table Editor

Browse and edit rows, and change the schema through migrations. Ships the `pgmeta` package (item 6). Every schema change is a migration file the developer can read before it is written.

| # | Item | Piece |
|---|---|---|
| 18 | Schema dropdown and table search covering tables, views and materialized views | Backend, frontend |
| 19 | Data grid with inline editing and typed cell editors (text, number, boolean, date and time, JSON, enum, array) and a referenced-row picker for foreign key columns | Frontend |
| 20 | Filter bar with `=`, `<>`, `>`, `<`, `>=`, `<=`, `like`, `ilike`, `in`, `is`; multi-column sort; filters and sort kept in the URL so a view can be shared or bookmarked | Backend, frontend |
| 21 | Pagination at 100, 500 or 1,000 rows, row count (estimated for large tables), refresh | Backend, frontend |
| 22 | Insert, edit, duplicate and delete rows; bulk select and delete with a confirmation naming the count | Backend, frontend |
| 23 | New table side panel: name, columns, primary key; creates a migration through the generator library with a diff preview | Backend, frontend |
| 24 | Column side panel: a type picker grouped by category including the app's enums, default suggestions (`now()`, `gen_random_uuid()`, literals), nullable, unique, identity, array and check; creates a migration | Backend, frontend |
| 25 | Foreign key picker with `ON DELETE` and `ON UPDATE` actions and a type check between the two columns before the migration is written | Backend, frontend |
| 26 | Definition view: the table's DDL in a read-only Monaco editor | Backend, frontend |
| 27 | CSV import with preview and column mapping; export as CSV, JSON or SQL inserts | Backend, frontend |
| 28 | System tables (goose, River, auth internals) are read-only unless unlocked for the session, with the reason shown | Frontend |

Docs: a Table Editor guide; the [database guide](guides/database.md) explains migrations made from the portal.

## Phase 3: SQL Editor

Run SQL against the development database with the guards a developer wants and none a production database would need. Ships the SQLite store (item 7) for history.

| # | Item | Piece |
|---|---|---|
| 29 | Monaco with schema autocomplete (tables, columns, functions), format, ⌘Enter to run, run selection only | Frontend |
| 30 | Snippet tree: Favorites, Project (`.orb/queries/*.sql`, committed to git and shared with the team), Local (this machine only) | Backend, frontend |
| 31 | Query history (SQL, duration, row count, errors) in `.orb/portal.db`, with run again | Backend, frontend |
| 32 | Results grid and a chart tab; copy or download as CSV, JSON or Markdown | Frontend |
| 33 | A row limit selector added to the query automatically when it has none, shown in the editor so nothing is hidden | Backend, frontend |
| 34 | Destructive-query warnings before running: `DROP`, `TRUNCATE`, `DELETE` or `UPDATE` without `WHERE`, `DROP COLUMN` | Frontend |
| 35 | Run in a transaction and roll back: see the effect of a statement without keeping it | Backend, frontend |
| 36 | `EXPLAIN` visualiser: plan nodes as a tree with cost, rows and timing, the slowest node highlighted | Backend, frontend |
| 37 | Templates: slow queries, index usage, table bloat, connections, locks, River queue statistics | Frontend |
| 38 | Save DDL as a migration: a statement that changes the schema can be written to `db/migrations` instead of run, with the diff preview | Backend, frontend |

Docs: a SQL Editor guide; `.orb/` layout (what is committed, what is ignored) in the [app internals guide](guides/app-internals.md).

## Phase 4: Schema visualiser, objects, migrations

| # | Item | Piece |
|---|---|---|
| 39 | Entity-relationship diagram (React Flow with dagre layout): tables as nodes with primary key, nullable and unique icons per column, and foreign key edges drawn between the columns they join | Frontend |
| 40 | Schema filter, find table, saved node positions (in `.orb/`, ignored by git), auto-layout, export as PNG, SVG or Mermaid, click-through to the Table Editor | Frontend |
| 41 | Objects pages: functions, triggers, enums, extensions, indexes (with usage counts and unused-index warnings from `pg_stat_user_indexes`), views. Changes create migrations | Backend, frontend |
| 42 | Migrations page: applied and pending, SQL preview, create (blank or from the diff between the schema and the migrations), up, down, redo, and an automatic up → down → up check that reports a migration whose down doesn't undo its up | Backend, frontend |

Docs: the [database guide](guides/database.md) gains the migration check; a schema page in the guides.

## Phase 5: Authentication

Backend: admin endpoints under `/ops/auth` for users, sessions, identities, passkeys and MFA, added to `modules/auth` with audit events, usable by any operator tool and not only the portal ([ADR-0038](adr/0038-authentication-v0-2.md), [ADR-0043](adr/0043-two-factor-authentication.md), [ADR-0044](adr/0044-passkeys.md)).

| # | Item | Piece |
|---|---|---|
| 43 | Users: list, search, create, edit, delete, verify email, ban and unban, impersonate (a development-only session whose audit event names the operator) | Backend, frontend |
| 44 | Providers (email and password, Google, Apple, GitHub) with configured or missing status, keys written to `.env` (never to the database), and a Test sign-in button; Apple needs a tunnel or the mock provider, and the page says which | Backend, frontend |
| 45 | Passkeys: list, revoke, register a test passkey against the local origin | Backend, frontend |
| 46 | Sessions: list per user, revoke one, revoke all | Backend, frontend |
| 47 | MFA: status per user, reset, regenerate recovery codes | Backend, frontend |
| 48 | Verification and password-reset codes shown from Dev Mail (Phase 9) or Mailpit until then, next to the user, so a flow can be finished without leaving the page | Frontend |
| 49 | Rate limits ([ADR-0052](adr/0052-shared-rate-limits.md)): counters by IP and user, reset one or all | Backend, frontend |
| 50 | Auth audit log: sign-ins, failures, provider links, MFA and passkey changes, filtered by user | Frontend |

Docs: an Auth page in the guides; [ADR-0026](adr/0026-operations-apis.md) amended with the `/ops/auth` surface; `api/` listings regenerated.

## Phase 6: Jobs

Jobs made in the portal are ordinary Go files produced by the generator, so they read, test and deploy like hand-written ones ([ADR-0033](adr/0033-background-jobs.md)).

| # | Item | Piece |
|---|---|---|
| 51 | Jobs list with the schedule in plain English ("every weekday at 09:00"), last and next run, active toggle | Backend, frontend |
| 52 | New job three ways: code (a Monaco template that becomes a real `.go` file), CLI (`orb gen job`, shown with the command to copy), form (name, queue, retries, timeout, cron from presets or raw; type: custom Go, HTTP request, SQL, send email template, dispatch another job) | Backend, frontend |
| 53 | The form writes normal Go through the generator with a diff preview, then reloads the app | Backend, frontend |
| 54 | Visual view for form-made jobs, read back from the generated file's marker comment; a job edited by hand is marked "Ejected — edit in code" and the form no longer offers to overwrite it | Backend, frontend |
| 55 | Job detail: run history, arguments, per-run logs, retry, cancel | Backend, frontend |
| 56 | Run now with JSON arguments, enqueue for later, and a scheduled view of upcoming runs | Backend, frontend |
| 57 | Queues: pause, resume, depth, throughput | Frontend |

Docs: the [background jobs guide](guides/background-jobs.md) gains "Jobs from the portal" and the ejection rule. Decided in [ADR-0071](adr/0071-job-kinds-and-ejection.md): kinds are rendered as ordinary Go by `orb gen job --kind`, the definition carries an `//orb:job` marker with the worker file's hash, and `GET /_portal/api/jobs` reports each job's kind and whether it is ejected.

## Phase 7: Logs

Backend ([ADR-0072](adr/0072-local-log-store.md)): structured fields on every request log record (`httpx.AccessLog`: `source`, `method`, `path`, `route`, `status`, `duration_ms`, `user_id` from the auth middleware through an `AccessNote`), `APP_LOG_FORMAT` so the app logs JSON under `orb dev`, and the supervisor stores the app's output in JSON Lines segments under `.orb/portal/logs` (bounded to 64 MiB) with substring search, so logs survive reloads (the `/_dev/logs` buffer doesn't).

| # | Item | Piece |
|---|---|---|
| 58 | Sources: HTTP, Auth, Jobs, Mail, Storage, Postgres (the container's JSON logs through Docker), App | Backend, frontend |
| 59 | Filters: time range, level, user (email or ID), method, path, status class, duration over X ms, request or trace ID, free text | Backend, frontend |
| 60 | Per-minute histogram above the list; click a bar to zoom the time range | Frontend |
| 61 | Live tail, a detail panel per record, "all logs for this request", saved filters | Backend, frontend |
| 62 | Errors grouped by fingerprint (message shape plus stack top) with count, first and last seen, the stack, and a link to the request | Backend, frontend |

Docs: the [observability guide](guides/observability.md) explains the local log store and its retention (size-bounded, cleared from Project Settings).

## Phase 8: Observability

Backend ([ADR-0073](adr/0073-observability-screen.md)): the screen builds on the app's request minutes (`/ops/observability`), `/ops/system`, the jobs overview and the audit statistics, plus what only `orb dev` can see: `pgmeta` statistics (`GET /_portal/api/db/stats`, `db/statements` from `pg_stat_statements`, which the development `compose.yaml` now preloads, `db/advice`), a `gopsutil` sampler (`GET /_portal/api/system`) and a health table across services (`GET /_portal/api/health`). The OTLP receiver inside `orb dev` sketched here was deferred: the request minutes and the log store answer the questions, and `orb dev --observability` (Grafana) stays for traces ([ADR-0028](adr/0028-local-development-environment.md)).

| # | Item | Piece |
|---|---|---|
| 63 | Service health table: app, PostgreSQL, mail, storage, with the last check and the reason when unhealthy | Backend, frontend |
| 64 | API: request rate, error rate, p50, p95 and p99 per route; slowest and most-failing routes | Backend, frontend |
| 65 | Database: connections against `max_connections` by application, pool statistics, cache hit ratio, table and index sizes, locks | Backend, frontend |
| 66 | Query performance from `pg_stat_statements` (enabled in the development `compose.yaml`) with `EXPLAIN` and index suggestions | Backend, frontend |
| 67 | System: CPU, memory, disk IO and usage, and the Go runtime (goroutines, heap, GC pauses) | Backend, frontend |
| 68 | Jobs and auth: throughput, failures, sign-ins by method | Backend, frontend |

Docs: the observability guide's local section rewritten around the portal; upgrade note for apps whose `compose.yaml` gains `pg_stat_statements`.

## Phase 9: Mail, env, configuration

| # | Item | Piece |
|---|---|---|
| 69 | Dev Mail: an SMTP catcher inside `orb dev` replacing Mailpit in `compose.yaml` ([ADR-0025](adr/0025-email-providers.md)); inbox, HTML, text and source views, code detection with copy, link extraction, delete, clear | Backend, frontend |
| 70 | Email template preview with sample data and a test send to the inbox | Backend, frontend |
| 71 | Env editor validated against the app's configuration schema ([ADR-0008](adr/0008-configuration.md)): secrets hidden until revealed, add, edit and delete, keys present in `.env.example` but missing from `.env` flagged, restart offered after a change | Backend, frontend |
| 72 | Feature flags ([ADR-0057](adr/0057-feature-flags.md)) and runtime settings through `/ops` | Frontend |

Docs: the [email guide](guides/email.md) and [environment variables guide](guides/environment-variables.md); upgrade note for the `compose.yaml` change (Mailpit removed, `MAIL_DELIVERY=devmail`). Decided in [ADR-0074](adr/0074-dev-mail-previews-and-env-editor.md): the catcher is `cli/internal/devmail`, previews come from the real message builders through the dev console, and `orb dev` edits `.env` in place.

## Phase 10: Storage

Backend: a new `modules/storage` with local disk, S3, DigitalOcean Spaces, Cloudflare R2 and MinIO drivers, its own ADR, and `orb add storage`. The portal is its first UI; the module stands on its own.

| # | Item | Piece |
|---|---|---|
| 73 | Buckets and driver status | Backend, frontend |
| 74 | File browser (column or list view): upload, download, delete, move, make directory; image and PDF preview | Backend, frontend |
| 75 | Signed URLs with an expiry; object metadata | Backend, frontend |
| 76 | Production-bucket guard: a red banner and read-only mode when the configured bucket isn't local, until unlocked for the session | Frontend |

Docs: a storage guide and ADR; `api/modules-storage.txt`. Decided in [ADR-0075](adr/0075-file-storage.md): two drivers (`local`, `s3` for every S3-compatible service), `/ops/storage…` as the portal's contract, `orb add storage`.

## Phase 11: Git

Runs the `git` on the developer's machine through `orb dev`; the portal never embeds its own git implementation, so hooks, signing and credentials behave as in the terminal.

| # | Item | Piece |
|---|---|---|
| 77 | Status: branch, ahead and behind, changed files | Backend, frontend |
| 78 | Diff viewer; stage and unstage files and hunks; commit with a message | Backend, frontend |
| 79 | Branches: create, switch, delete; pull, push, fetch | Backend, frontend |
| 80 | Merge preview, then merge; conflicts listed and opened in the editor | Backend, frontend |
| 81 | Log and history graph. Every destructive action (delete branch, discard changes, force operations) is confirmed with what it will lose | Backend, frontend |

Docs: a Git page in the guides, including what the portal never does (rewrite history, push with force). Decided in [ADR-0076](adr/0076-git-screen.md).

## Phase 12: Generators and scaffolding

| # | Item | Piece |
|---|---|---|
| 82 | Generators hub: resource (a fields form), job, migration, module, `orb add mail`, `orb add orgs`, each with a diff preview before apply, the same library as the CLI ([ADR-0021](adr/0021-generator-operation-model.md)) | Backend, frontend |
| 83 | `orb new` creates the project, runs `orb dev` and opens the portal, so the first run ends in the browser | Backend |
| 84 | Project settings: name, ports, database, CORS origins, mail, storage driver, API and service-account keys; danger zone: reset the database, clear the portal's logs and history | Backend, frontend |

Docs: the [CLI guide](guides/cli.md) and the quickstart updated for the new first run.

## Phase 13: Advanced

Each item gets its own decision record before it starts; the list records intent, not commitment.

| # | Item | Piece |
|---|---|---|
| 85 | Time-travel clock: move the app's clock forward to test crons, token expiry and trials, through a development-only clock source | Backend, frontend |
| 86 | Git-branch-aware database snapshots and named checkpoints: switching branches offers the snapshot taken on that branch | Backend, frontend |
| 87 | Causal timeline: one request followed through its SQL, the jobs it enqueued and the emails they sent | Backend, frontend |
| 88 | Recorded request to Go integration test: a request made in the builder becomes a test file in `test/e2e` | Backend, frontend |
| 89 | Pre-deploy readiness check: environment, pending migrations, routes without auth, breaking API changes against the last tag | Backend, frontend |
| 90 | Bug capsules (a request, its logs, SQL and app state in one file) and AI diagnosis: "why did this fail?", and plain English to SQL or to a cron expression, through the developer's own model key | Backend, frontend |

## What each phase must ship

| Part | Must include |
|---|---|
| **Backend** (Go) | Tests for every new endpoint and for the security checks (Host, loopback peer, token, production refusal); an amendment to the ADR whenever a public surface changes (`/ops` endpoints, module APIs, environment variables, `compose.yaml`); regenerated API listings through `internal/tools/apicheck` and passing `TestPublicSurface`, `TestOpsAPICompatible` and `TestGoldenAppsDontDrift` |
| **Frontend** (gorbital-dashboards) | `pnpm typecheck`, `pnpm build` (the static export must succeed, since it is what gets embedded) and `vitest` for the API client, the mock mode and every screen's data mapping; the export synced into the CLI with `scripts/sync-portal.sh` |
| **Docs** | The status table in this file; the guide for the area; `CHANGELOG.md`; an upgrade note in [upgrade notes](guides/upgrade-notes.md) whenever generated apps change (new `.env` variables, `compose.yaml`, generated files) |
| **Then** | Push `dev-portal/phase-N` in both repositories |

A phase isn't done while any of its numbered items is missing or shown only on mock data, unless the table above says it ships later.

## Not included

- A row-level security policy editor: policies are written in migrations ([ADR-0061](adr/0061-row-level-security.md)), and the Table Editor shows them read-only.
- Realtime (change subscriptions), edge functions and billing: Supabase Studio features with no counterpart in gorbital.
- Any hosted or remote mode: the portal runs on the developer's machine against the development database only.
- A production admin UI: that is a client template ([ADR-0047](adr/0047-client-templates.md)), not the portal.

## Reference

Supabase Studio (Apache-2.0) was studied for the table editor, the SQL editor, the schema visualiser, the logs previewer and the cron form. Ideas are borrowed: interaction patterns, the filter operators, the grouping of column types, the destructive-query warnings. Code is not copied unless the file keeps its licence notice and the copy is listed in `NOTICE`.
