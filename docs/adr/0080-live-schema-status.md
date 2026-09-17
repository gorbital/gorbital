# ADR-0080: Live schema status

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0066, ADR-0069

## Context

The database's schema changes from three places: migration files under `db/migrations` (written by hand, by `orb gen`, or pulled from git), the Dev Portal's plans (the Table Editor, Objects and Migrations screens write a file and ask `orb dev` to apply it, [ADR-0067](0067-table-editor-and-pgmeta.md), [ADR-0069](0069-schema-visualiser-objects-and-migrations.md)), and the SQL editor in commit mode ([ADR-0068](0068-sql-editor.md)). The portal learned about a change only when a screen refetched, and `orb dev` told the developer nothing in three cases that go wrong quietly:

| Case | Today | Evidence |
|---|---|---|
| A migration file changes under `orb dev --no-reload` | Nothing is watched; the file waits, unapplied, until the next `orb dev` | `loop` returns before the watcher when reload is off (`cli/internal/cli/dev.go`) |
| A migration file changes while the app is stopped or the build is broken | The rebuild fails before the migrate step; the state says `build failed`, not that a migration waits | `rebuild` |
| An applied migration file is edited | goose applies a version once and never looks at the file again; PostgreSQL keeps the old schema and the developer sees their edit "not working" | `docs/start/troubleshooting.md` tells them to `docker compose down -v` |

The portal also had no way to know that the SQL editor just created a table: the Schema, Objects and Table Editor screens kept showing the old catalog until reloaded.

## Options

### What tells the portal

| Option | Verdict |
|---|---|
| Every screen polls `db/migrations` and the catalog | Rejected: a query per screen per interval against the developer's database, and still a delay |
| PostgreSQL event triggers or `LISTEN` on DDL | Rejected: needs objects in the app's database that the migrations don't own, and says nothing about files |
| **`orb dev` publishes a `schema` event on the existing stream whenever it knows the schema may have changed (a migrate run, a file change, a committed DDL from the SQL editor, startup), with the same object served at `GET /_portal/api/db/schema-status` on demand** | **Chosen**: `orb dev` is the one process that sees every cause; the event carries the answer, not a hint to go and ask |

### How an edited file is found

| Option | Verdict |
|---|---|
| Compare the file's modification time with goose's `tstamp` | Rejected: a `git checkout` or a `touch` rewrites times without changing content, and a merge can apply a change with an old time |
| Ask PostgreSQL what was applied | Rejected: goose records the version and the time, never the content |
| **Record the SHA-256 of each applied file's content in `.orb/portal/migrations.json` after every successful migrate run (a file seen applied for the first time is recorded with its current content: the baseline for apps that ran before this ADR); a file whose hash no longer matches is `edited`** | **Chosen**: the record is the developer's own (git ignores `.orb`), cheap to keep, and says exactly what is wrong |

### What the watcher does without reload

| Option | Verdict |
|---|---|
| Apply the migration anyway | Rejected: `--no-reload` was asked for; a migration changes the database under a running app |
| **Watch `db/migrations` with its own snapshot, independent of the Go-file snapshot; on a change publish the status with `source: "code"` and `needs_restart: true`, and print a line naming the file and what applies it (Dev Portal → Restart); the portal's commands (restart, migrate) now run without reload too** | **Chosen** |

## Decision

- `portal.SchemaStatus` and the `schema` event. `Event` gains `Schema *SchemaStatus`; `Hub.SetSchema` keeps the latest status and publishes it, and a new event stream sends it right after the first `state` event. The status: `database`; `source` (`startup`, `code`, `migrate`, `portal`, `sql`); `checked_at`; `applied` (the files the run that caused the status applied); `pending` (`file`, `version`, `reason`: `new`, or `out_of_order` when the version is lower than the highest applied one, which goose won't apply in order); `edited` (`file`, `version`); `needs_restart` (pending files `orb dev` won't apply on its own: no reload, the app stopped or failed, or the last migrate failed); `problem` (the last migrate error). An app without a database answers `{"database": false}`.
- `GET /_portal/api/db/schema-status` computes the status now (the files, goose's table, the hashes), keeping the `source` and `applied` of the last published status so a poll and the stream agree.
- `orb dev`: every migrate run (startup, a rebuild, the portal's apply, roll back, redo and reset) records the applied content and publishes the status (`startup`, `migrate` or `portal`); a change to `db/migrations` that the run didn't apply publishes `code` with a console warning; the SQL editor publishes `sql` after a committed script with CREATE, ALTER, DROP, TRUNCATE or COMMENT (`pgmeta.IsDDL`; a rolled-back script changed nothing). An applied file found edited is named on the console after every code change and when first found.
- The portal shows a banner from the status: pending files with Restart (or Apply pending) when `needs_restart`, edited files with Redo, the last migrate error; the Schema, Objects, Migrations and Table Editor screens refetch on every `schema` event.

## Consequences

- A migration written in the editor is reflected in the portal within the watch interval, whichever way `orb dev` runs, and the developer is told when it will not be applied on its own.
- An edited applied file stops being a mystery: the status names it, and the fix is the one that was always right (Redo in development, or a new migration).
- `--no-reload` now serves the portal's restart and migrate requests, which it silently dropped before.
- The record is a cache: deleting `.orb/portal/migrations.json` loses only the edited-file check until the next migrate run baselines the files again. A migration applied outside `orb dev` (`go run ./cmd/migrate` in another terminal) shows as applied at once and is baselined at the next run.
- `applied` says what one run did; it is not a list of every applied file, which `GET /_portal/api/db/migrations` remains.
- Open: a watch on the catalog itself for changes made by other clients (psql), which nothing in `orb dev` sees; a `schema` event when a migration file is deleted while its version stays applied.
