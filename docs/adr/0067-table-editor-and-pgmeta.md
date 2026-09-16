# ADR-0067: Table Editor and `pgmeta`

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0066, ADR-0029, ADR-0005

## Context

Phase 2 of the [Dev Portal roadmap](../dev-portal-roadmap.md) is the Table Editor: browse a Full app's PostgreSQL database, edit rows, and change the schema from the browser, as Supabase Studio does, without the developer writing SQL by hand. Today:

| Area | Today | Evidence |
|---|---|---|
| Database access from `orb` | None: the CLI has no database dependency; migrations run through the app's `cmd/migrate` (goose, ADR-0005) | `cli/go.mod` |
| Migrations | Forward only, hand-written SQL under `db/migrations`, applied by `orb dev` when a file changes; released migrations never change | ADR-0005, `docs/guides/database.md` |
| The portal | Reads the app's state and `/_dev/`, `/ops/` data; can write generated files as plans (ADR-0066) | `cli/internal/portal` |
| The database's other tables | goose's version table, River's queue tables, the modules' tables (`auth_*`, `audit_events`, …), extensions' tables | `modules/postgres/migrate.go`, `modules/jobs/migrate.go` |
| Studio | Reads the catalog through a `pg-meta` service that builds SQL, edits rows through PostgREST, and runs DDL directly against the database, leaving the migration history behind | supabase/postgres-meta (Apache-2.0) |

## Options

### Where the database code runs

| Option | Verdict |
|---|---|
| In the app, behind `/_dev/` | Rejected: writes from the console break ADR-0065's GET-only rule, and the app's pool would serve the editor's long queries |
| **In `orb dev`, a new `cli/internal/pgmeta` package on pgx, connected with `DATABASE_URL` from `.env` on first use (two connections, UTC, statement timeouts)** | **Chosen**: the portal already lives there; the CLI gains one dependency (pgx, already used by every Full app) and nothing reaches production |

### Schema changes

| Option | Verdict |
|---|---|
| Run DDL against the database, as Studio does | Rejected: the local database would drift from `db/migrations`, and teammates and production would never get the change |
| **Every schema change is a plan (`pgmeta.Plan`: Up statements, Down statements, notes) rendered as a goose migration file and applied by the app's own `cmd/migrate` through the supervisor; applying goes through the clean-git check like the generators** | **Chosen**: the migration is in git before the local database changes; a plan is shown before anything is written |
| A Down section in generated migrations | Chosen, as goose syntax allows: the app's migrations stay forward-only in practice (ADR-0005), but a portal-written Down lets the developer undo a mistake in development. Changes that lose data (drop table, drop column, an enum value) are marked irreversible and their Down is a comment |

### Rows

| Option | Verdict |
|---|---|
| JSON values typed in Go | Rejected: int8 and numeric lose precision in JSON numbers, and every type needs a codec |
| **Every column is read as text (`::text`) and every value written as a text parameter the server casts; identifiers come only from the catalog and are quoted with `pgx.Identifier`; filter operators are a fixed set; rows are keyed by the primary key, and tables without one are read-only** | **Chosen**: any type round-trips unchanged, no SQL is built from request strings, and an edit that would touch a different number of rows than expected rolls back |

### Which tables the portal may change

| Option | Verdict |
|---|---|
| Everything | Rejected: goose's, River's and extensions' tables belong to their tools |
| **Three ownerships from the catalog and name prefixes: `user` (the app's own; rows and schema editable), `managed` (a gorbital module's, by prefix such as `auth_`; rows editable with a warning, schema changes refused), `system` (system schemas, extensions, `goose_db_version`, `river_*`; read-only)** | **Chosen**: the app's migrations own its tables; the framework's own its; tools' are shown, not touched |

## Decision

### `cli/internal/pgmeta`

| Piece | Decision |
|---|---|
| Catalog | `Schemas`, `Tables` (with row estimate, live rows, size, comment, RLS, extension ownership), `Detail` (columns with formatted type, nullability, default and generation expressions, identity, enum values, comment, primary key, unique, foreign key targets; constraints with `pg_get_constraintdef`, referenced columns and actions; indexes with usage; triggers; the primary key), `ForeignKeys` (the schema graph), `Enums`, `Functions`, `Views`, `Extensions`, `Types` (the picker: built-in types with descriptions and default suggestions, plus the enums), `ServerVersion`. Queries use `pg_catalog` only and allow for PostgreSQL 18 (`contype = 'n'`, virtual generated columns). Adapted from postgres-meta with attribution |
| Rows | `Query` (filters `= <> > < >= <= ~~ ~~* in is`, sorts with the key as tiebreaker, limit up to 1,000, count exact up to 100,000 estimated rows and the planner's estimate beyond or on timeout), `Insert`, `InsertMany` (one transaction, at most 1,000), `Update` (one row by key), `Delete` (rows by key; refused when any is missing). Cells are `*string`; nil is NULL |
| Plans | `Plan(ctx, Change)` validates against the catalog (names, types, enum references, ownership `user`) and returns `Plan`. Kinds: create_table, drop_table, rename_table, add_column, drop_column, rename_column, alter_column (nullability, type with `USING` cast, default, identity, unique, comment, in that order), add_foreign_key, add_unique, add_check, drop_constraint, set_primary_key, create_index (`CONCURRENTLY` marks the file `NO TRANSACTION`), drop_index, create_enum, add_enum_value, rename_enum_value, comment, rls. `Render` writes the goose file: summary, `-- +goose Up`, `-- +goose Down` with notes and an `irreversible` marker |
| Errors | `ErrNotFound`, `ErrUnknownColumn`, `ErrUnknownOperator`, `ErrInvalidInput` (the server's message for SQLSTATE classes 22, 23 and 42), `ErrNoPrimaryKey`, `ErrRowCount`, `ErrSystemTable` |

### The portal

| Piece | Decision |
|---|---|
| `Config.Database` | `Open` (lazy, cached for the run), `NextMigrationVersion` (the generators' rule: now, or one more than the newest), `Apply` (clean-git check unless `allow_dirty`, `genplan.Apply`, then the supervisor's `Migrate`) |
| `GET /_portal/api/db/schemas`, `tables[?schema=…]`, `tables/{schema}/{table}`, `foreign-keys`, `enums`, `functions`, `views`, `extensions`, `types` | The catalog; without `?schema=` every non-system schema |
| `POST /_portal/api/db/rows/query`, `insert`, `import`, `update`, `delete` | Rows, with `pgmeta`'s request shapes; unknown JSON fields refused |
| `POST /_portal/api/db/ddl/plan` and `apply` | `{"change": …, "name": …, "allow_dirty": …}` → the plan and the migration file (path and content); `apply` writes it and queues the migration |
| Errors | 404 `not_found` and `no_database` (Minimal apps), 422 `invalid_input`, 409 `no_primary_key`, `row_count`, `plan_conflict`, 403 `system_table`, 503 `database_unavailable`, 504 `database_timeout` |
| Status | `portal.database` reports whether the app has a database |

### The UI (gorbital-dashboards)

Database → Table Editor: a schema dropdown and table search; a grid with inline editing and typed cell editors; a filter bar and multi-column sort kept in the URL; pagination; insert, edit, duplicate and delete rows with bulk selection; New table and Column side panels that show the migration before writing it; a Definition view; CSV import (parsed in the browser, sent in batches to `rows/import`) and export; system and managed tables read-only unless unlocked.

## Why

- The plan-then-migrate rule keeps the database and `db/migrations` in step: what the portal does is what a teammate gets from `git pull` and `orb dev`.
- Reading and writing every value as text is the only approach that treats every PostgreSQL type the same way and never rounds a number.
- Identifiers from the catalog and values as parameters leave no request string in SQL; the ownership classes keep the portal away from tables it didn't create.
- The Down side costs nothing to generate for reversible changes and makes development mistakes cheap; irreversible ones are named as such.

## Trade-offs

- `orb` now depends on pgx; the binary grows by a few megabytes.
- Counts on large tables are estimates; the UI says so.
- Managed tables' schemas can't be changed from the portal; that is the framework's migrations' job.
- A migration file the portal writes and the developer then edits by hand before it runs is applied as edited; the portal never rewrites it.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)) row 11 extends to the database API: the same token and checks guard it; SQL is built only from catalog identifiers and parameters.
- The [Dev Portal guide](../guides/dev-portal.md) lists the database endpoints and the Table Editor; the [database guide](../guides/database.md) notes portal-written migrations.
- Phase 4 (schema visualiser, objects, migrations) builds on `ForeignKeys`, `Functions`, `Views`, `Extensions` and the plan kinds for enums and indexes.

## Implementation notes (2026-09-16)

- `cli/internal/pgmeta` with database tests (`GORBITAL_TEST_DATABASE_URL`, skipped without it, required in CI): the catalog against a schema with an enum, foreign keys, arrays, jsonb, comments and a view; rows with every operator, bad values, edits by key, refused edits; every plan kind applied Up then Down against the database; ownership of `river_*` and `auth_*` tables.
- `cli/internal/portal/db.go` with handler tests over a fake database, including the plan and apply flow writing a migration file and queuing the migrate command.
- `orb dev` opens the database on the first portal request that needs it and closes it when it stops.
