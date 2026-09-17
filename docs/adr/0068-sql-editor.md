# ADR-0068: SQL editor

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0066, ADR-0067

## Context

Phase 3 of the [Dev Portal roadmap](../dev-portal-roadmap.md) is the SQL editor: run queries against the development database from the browser, keep the ones worth keeping, see what a query costs, and turn tested DDL into a migration. The Table Editor (ADR-0067) gave `orb dev` a database connection and a way to write migrations; this record decides how scripts run and where queries live.

## Options

### Running a script

| Option | Verdict |
|---|---|
| Statement by statement through the extended protocol | Rejected: developers paste scripts with several statements, `DO` blocks and dollar quotes; splitting them in Go is a parser's job |
| **The whole script through the simple protocol (`PgConn.Exec`) inside one transaction, every result set returned as text cells like the Table Editor's; the transaction is rolled back unless the developer chose to commit, or runs read-only** | **Chosen**: the server parses; results arrive per statement in text, so no type registry is needed; rolling back by default means a query can be tried safely |
| Letting a script manage its own transaction in rollback mode | Rejected: a `COMMIT` inside would end the portal's transaction; scripts with `BEGIN`, `COMMIT`, `ROLLBACK` or `END` are refused unless the mode is commit |

### Warnings

A regex pass over the script (comments and string literals blanked) flags `DROP`, `TRUNCATE`, `DELETE` and `UPDATE` without `WHERE`, dropped columns and type changes, with the line each starts on. It is advice before running, as Studio's is, not a guard: dollar-quoted bodies can hide statements from it. Rollback mode is the guard.

### Where queries live

| Option | Verdict |
|---|---|
| A database of the portal's own | Rejected for snippets: a query worth keeping is worth sharing, and a file in git is how this project shares things |
| **Snippets as files under `db/queries/<name>.sql`, committed with the app; favourites and the run history under `.orb/portal/`, which git ignores, as the developer's own** | **Chosen**: teammates get the queries from `git pull`; nobody gets another developer's history. History is a JSON Lines file capped at 500 runs; no SQLite yet (the roadmap's local store waits for a need it can't meet) |

### Explain

`EXPLAIN (FORMAT JSON, VERBOSE)`, with `ANALYZE, BUFFERS` on request, always in a transaction that is rolled back, so explaining an `UPDATE` with `ANALYZE` changes nothing.

## Decision

| Piece | Decision |
|---|---|
| `pgmeta.Run(ctx, RunRequest)` | Modes `rollback` (default), `commit`, `readonly`; `row_limit` (500, at most 10,000) truncates each result set and says so; `timeout_seconds` (30, at most 300) as `statement_timeout`. Returns every statement's command tag, columns, rows as text cells and rows affected; on error, the server's message, SQLSTATE, detail, hint, byte position and line; whether it committed or rolled back; the duration; and the warnings. A server error is data in the result, not an HTTP error |
| `pgmeta.Explain(ctx, sql, analyze)` | The plan as PostgreSQL's JSON |
| `pgmeta.Check(script)` | The warnings above |
| `pgmeta.Templates()` | Ready-made queries: table sizes, index usage, missing indexes, bloat, connections, locks, slow queries (needs `pg_stat_statements`), River queues and failed jobs, recent audit events, migrations, extensions |
| `portal.SQLStore` | `Snippets`, `Save(name, sql, favorite)`, `Delete`; `Record`, `History`, `ClearHistory`. Names are letters, digits, hyphens and underscores, so a name can't leave `db/queries` |
| Endpoints under `/_portal/api/db/sql/` | `GET templates`, `POST check`, `POST run`, `POST explain`, `GET snippets`, `PUT snippets/{name}`, `DELETE snippets/{name}`, `GET history`, `DELETE history`, `POST migration` (a script saved as a migration's Up section, optionally applied through the supervisor). Every run is recorded in the history |
| The UI | Database → SQL Editor: Monaco with the schema's tables and columns for completion, ⌘Enter to run, running the selection only, a mode switch (rollback, commit, read-only) with the warnings shown before a run, results as tabs per statement with copy and download, an EXPLAIN view, the snippet tree (favourites, project, templates), the history, and Save as migration |

## Why

- One transaction per run, rolled back by default, is the simplest safe default: try anything, keep nothing until asked.
- Text results reuse the Table Editor's rendering and lose nothing.
- Files under `db/queries` follow the project's rule that what the portal makes is what git holds.

## Trade-offs

- A script's `COMMIT` can't be honoured in rollback mode; the editor says to switch modes.
- The warnings are heuristics.
- The history is per machine and per app directory; moving the checkout loses it.

## Consequences

- The [Dev Portal guide](../guides/dev-portal.md) lists the endpoints and the screen; the [database guide](../guides/database.md) mentions `db/queries`.
- Threat model row 11: the same checks guard the runner; the developer's own SQL runs with the app's database role, as `psql` would.

## Implementation notes (2026-09-16)

`cli/internal/pgmeta/sql.go` with database tests (modes, errors with positions, truncation, timeouts, every template, explain with and without analyze, the warnings); `cli/internal/portal/sqlstore.go` and `sql.go` with handler tests over a fake runner and a temporary app directory (snippets on disk, history capped, names refused, no database).
