# ADR-0069: Schema visualiser, database objects and migrations

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0005, ADR-0017, ADR-0067

## Context

Phase 4 of the [Dev Portal roadmap](../dev-portal-roadmap.md) shows the database as a whole: the tables and their foreign keys as a diagram, the other objects (functions, triggers, enums, extensions, indexes, views) with a way to create and drop them, and the migrations with a way to apply, roll back and check them. Today:

| Area | Today | Evidence |
|---|---|---|
| Catalog | `pgmeta` reads foreign keys, enums, functions, views, extensions, indexes and triggers | ADR-0067 |
| Plans | Kinds for tables, columns, constraints, indexes and enum values; nothing for extensions, functions, triggers or views | `cli/internal/pgmeta/ddl.go` |
| Migrations | Forward only: `cmd/migrate` applies pending files; `--status` reports; the library has `Migrate` and `Migrations`; released migrations never change | ADR-0005, ADR-0017, `modules/postgres/migrate.go` |
| The portal | `POST /_portal/api/app/migrate` applies pending migrations; the Database page shows the state | ADR-0066, ADR-0067 |

## Options

### Rolling back

| Option | Verdict |
|---|---|
| Keep migrations forward only everywhere | Rejected for development: a migration the portal wrote a minute ago, with a Down it rendered, should be undoable without a second migration; ADR-0005's rule protects released migrations and production, not a developer's own working tree |
| **`postgres.MigrateDown` (one step) and `MigrationList`; `cmd/migrate --down` and `--redo` in Full apps, refused when `APP_ENV` is production; the portal's `migrate-down` and `migrate-redo` actions through the supervisor** | **Chosen**: development gets an undo and a check (redo runs Down then Up, which is what proves a Down works); production keeps the rule |

### Objects

Plan kinds `create_extension`, `drop_extension`, `create_function`, `drop_function`, `create_trigger`, `drop_trigger`, `create_view`, `drop_view` and `drop_enum` render the statements as the others do: a definition the developer writes (the whole `CREATE FUNCTION` or `CREATE TRIGGER` statement, or a view's `SELECT`) becomes the Up, and the catalog's definition becomes the Down of a drop when the UI passes it; without it the drop is marked irreversible. Functions are dropped by signature (`name(argument types)`, the catalog's `identity_args`).

### The diagram

Drawn in the browser from `GET /_portal/api/db/tables`, `tables/{schema}/{table}` and `foreign-keys` with React Flow and dagre; positions the developer drags are kept in the browser; export to PNG, SVG and Mermaid is the browser's. The backend adds nothing.

## Decision

| Piece | Decision |
|---|---|
| `postgres.MigrateDown(ctx, pool, fsys)` | Rolls back the most recently applied migration, returning its version, 0 when none; the advisory lock and `WithoutRowLevelSecurity` as `Migrate` |
| `postgres.MigrationList(ctx, pool, fsys)` | Every file with its version, path, whether applied and when |
| Full apps | `app.MigrateDown` refuses in production; `cmd/migrate --down` rolls back one, `--redo` rolls back one and applies pending; River's tables are never rolled back |
| `pgmeta.Migrations(ctx, dir)` | The files under `db/migrations` with their SQL, whether they have a Down section, and their state from `goose_db_version`; versions in the table without a file are listed too |
| The portal | `GET /_portal/api/db/migrations`; `POST /_portal/api/app/migrate-down` and `migrate-redo` (202, through the supervisor; the outcome arrives as state events and the migrations list); the object plan kinds through `ddl/plan` and `ddl/apply` |
| The UI | Database → Schema (the diagram), Objects (functions, triggers, enums, extensions, indexes, views, with create and drop as migrations), Migrations (the list with SQL, apply, roll back, redo) |

## Why

- Down and redo in development make portal-written migrations safe to try; refusing them in production keeps ADR-0005 intact where it matters.
- Object definitions as the developer wrote them, with the catalog's definition for the Down, keep the plan rule (a file in git before the database changes) without a DDL generator for every object kind.

## Trade-offs

- `--redo` rolls back and re-applies only the most recent migration; older ones need repeated `--down`.
- A migration without a Down section rolls back as a versioned no-op: goose records it as unapplied while its objects remain; the list flags files without a Down.

## Consequences

- Upgrade notes: Full apps get `cmd/migrate --down` and `--redo` and `internal/app/migrate.go`'s `MigrateDown` through `orb upgrade`; the API listing of `modules/postgres` gains the two functions.
- The [database guide](../guides/database.md) and the [CLI guide](../guides/cli.md) describe the flags; the [Dev Portal guide](../guides/dev-portal.md) the endpoints and screens.

## Implementation notes (2026-09-16)

`modules/postgres` tests roll back one step at a time and re-apply; `pgmeta` tests apply every new plan kind Up then Down against the database and list migration files with and without a Down section; portal handler tests cover the list and the two actions.
