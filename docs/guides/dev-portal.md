# Dev Portal

While you run an app with `orb dev`, it also serves the Dev Portal at http://127.0.0.1:3100: a web UI for the app on your bench. It shows what the app is doing and its output as it happens, lets you restart it, and reaches the app's development APIs (routes, requests, logs, email, jobs, configuration) without you holding any token. The database, SQL editor, schema, authentication, storage and git screens are all there too ([Screens](#screens)); what is still planned is in the [Dev Portal roadmap](../dev-portal-roadmap.md). Decision: [ADR-0066](../adr/0066-dev-portal.md). The [Dev Portal section](../dev-portal/index.md) of the documentation has a page per screen, with screenshots.

The portal exists only while `orb dev` runs, only on this machine, and only in development. Nothing about it is in the app or in production builds.

## Opening it

`orb dev` prints a link and opens it in your browser:

```text
  ✓ API        http://127.0.0.1:8080
  ✓ API docs   http://127.0.0.1:8080/docs
  ✓ Emails     http://127.0.0.1:3100/mail (caught at 127.0.0.1:1025)
  ✓ Dev APIs   http://127.0.0.1:8080/_dev/ (docs/guides/dev-console.md)
    Token      q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E (Authorization: Bearer; new on every orb dev run)
  ✓ Dev Portal http://127.0.0.1:3100/_portal/auth?t=Rk1…vQ (docs/guides/dev-portal.md)
```

The link holds this run's portal token. Opening it once sets a cookie in your browser, and the portal at http://127.0.0.1:3100 works for the rest of the run. The next `orb dev` run prints a new link; a link from an earlier run shows a page saying so.

| Flag or variable | Effect |
|---|---|
| `--no-open` | Print the link without opening a browser (also the behaviour in CI and when output isn't a terminal) |
| `--no-portal` | Don't serve the portal |
| `--portal-port 3110` | Listen on another port |
| `DEV_PORTAL_PORT=3110` in `.env` | The same, for good |
| `DEV_PORTAL_TOKEN` in the shell running `orb dev` | Use that token instead of a random one, for a tool that needs the same token across runs; it must be 32 to 512 visible ASCII characters and is never written to `.env` |
| `--tunnel quick`, `--tunnel named` (`--tunnel-hostname`) | Also start a [tunnel](../dev-portal/tunnel.md) to the app with your cloudflared |

A taken port stops `orb dev` before anything starts, naming the port and the ways to move it.
The portal is for development: `APP_ENV=production` in `.env` or the environment stops `orb dev` too, because the portal reads the database and changes the project. Pass `--no-portal` to run the app anyway.

### Landing on a page

The link accepts `next`, a path of the portal to land on once the cookie is set: `http://127.0.0.1:3100/_portal/auth?t=<token>&next=/database/schema`. Only paths of the portal itself are accepted; anything else lands on the Overview.

### Light and dark theme

The portal follows your system's colour scheme the first time you open it (dark when the browser states no preference). The sun or moon button in the top bar, next to the notifications bell, switches between the dark and the light theme, as does "Toggle theme" in the ⌘K palette; the choice is remembered per browser (`localStorage.theme`) and applied before the first paint, so pages never flash the other theme. Every screen, the charts, the SQL editor and the schema diagram render in both.

## What it serves

| Path | What |
|---|---|
| `/` and every page | The portal's UI, a Next.js static export embedded in `orb` (built from [gorbital-dashboards](https://github.com/gorbital/gorbital-dashboards)). Pages are open to any local reader and hold nothing secret; every request for data needs the token |
| `/_portal/auth?t=<token>` | Signs the browser in: sets the `orb_portal` cookie and goes to `/` |
| `/_portal/api/…` | The portal's own API: the app's state and output, restarts, generators (below) |
| `/_portal/app/…` | A proxy to the app: `/_portal/app/v1/ping` is the app's `/v1/ping`. Requests to `/_portal/app/_dev/…` and `/_portal/app/ops/…` that carry no `Authorization` get the [dev console](dev-console.md) token added by `orb dev`, so the UI never sees it: in Full apps the token acts as the development operator on `/ops/` ([ADR-0066](../adr/0066-dev-portal.md)). Other requests go through with the headers and cookies you send, minus the portal's own |

Every API and proxy request must pass, in order: a `Host` header naming `localhost`, `127.0.0.1` or `[::1]` (403 otherwise: this defeats DNS rebinding, since a page on another site that points its own name at `127.0.0.1` still sends its own name); a connection from this machine (403); the token, as the cookie or as `Authorization: Bearer <token>` (401); and, for anything but GET and HEAD, an `X-Orb-Portal` header (403), which a browser sends only after a CORS preflight the portal never answers. Responses never carry CORS headers.

### Calling the API yourself

```bash
TOKEN=Rk1…vQ                       # from the link orb dev printed
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3100/_portal/api/status
curl -H "Authorization: Bearer $TOKEN" 'http://127.0.0.1:3100/_portal/api/output?limit=50'
curl -N -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3100/_portal/api/events
curl -X POST -H "Authorization: Bearer $TOKEN" -H "X-Orb-Portal: 1" http://127.0.0.1:3100/_portal/api/app/restart
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:3100/_portal/app/_dev/routes
```

| Endpoint | Returns |
|---|---|
| `GET /_portal/api/status` | `portal` (orb version, whether a UI is bundled, start time), `project` (name, module, preset, tenancy, features, mail provider, directory, whether it has a database), `app` (below), `links` (`api`, `docs`, and `mail`, `console`, `grafana` when they apply), `generators` (names) |
| `GET /_portal/api/output?limit=200` | The most recent lines the app and `orb dev` wrote, oldest first: `{"time", "stream": "app"\|"orb", "text"}`. `orb dev` keeps 2,000 |
| `GET /_portal/api/events` | Server-Sent Events: a `state` event first, then the latest `schema` status (below) and, while a tunnel runs, its `tunnel` status, then `state`, `output`, `schema` and `tunnel` events as they happen, `: keep-alive` every 15 seconds, `dropped` with a count when the client fell behind, and a final `end` after 30 minutes or when `orb dev` stops. At most 8 streams at once |
| `POST /_portal/api/app/restart` | Rebuilds and restarts the app; 202 with the status. `stop` ends the process and leaves it stopped until `start` or a file change; `start` starts a stopped app without rebuilding; `migrate` applies pending migrations (`go run ./cmd/api migrate`, or `go run ./cmd/migrate` in a v0.1 app) without a restart, 409 in an app without a database. 409 while an earlier request is still being handled |
| `POST /_portal/api/generators/{name}/plan` | Body `{"input": {…}}`. Answers the plan: every file the generator would write, with its content (and the current content of files it changes), the summary `orb gen` shows, and the next steps. Nothing is written |
| `POST /_portal/api/generators/{name}/apply` | The same body, plus `"allow_dirty": true` to skip the clean-git check. Plans again and writes; 409 `plan_conflict` if a file changed since the plan |
| `GET /_portal/api/db/schemas`, `db/tables?schema=public`, `db/tables/{schema}/{table}`, `db/foreign-keys`, `db/enums`, `db/functions`, `db/views`, `db/extensions`, `db/types` | The database's catalog, read with `DATABASE_URL` from `.env` ([ADR-0067](../adr/0067-table-editor-and-pgmeta.md)); without `?schema=`, every schema that isn't PostgreSQL's own. Each table carries an `ownership`: `user` (the app's), `managed` (a gorbital module's; rows editable, schema not), `system` (goose, River, extensions; read-only). 404 `no_database` in a Minimal app; 503 `database_unavailable` when PostgreSQL doesn't answer |
| `POST /_portal/api/db/rows/query` | `{"schema", "table", "filters": [{"column", "operator", "value"}], "sorts": [{"column", "descending"}], "limit", "offset"}`; operators `= <> > < >= <= ~~ ~~* in is`. Answers the columns, the primary key, rows as arrays of text cells (`null` is NULL), and a count (`estimated` for large tables). Every value is text as PostgreSQL prints it, and is sent back the same way |
| `POST /_portal/api/db/rows/insert`, `import`, `update`, `delete` | Rows by primary key (`keys`), values as text (`values`); tables without a primary key are read-only (409 `no_primary_key`); an edit that would touch an unexpected number of rows is rolled back (409 `row_count`); the server's complaint about a value comes back as 422 `invalid_input` |
| `POST /_portal/api/db/sql/run` | `{"sql": "…", "mode": "rollback" \| "commit" \| "readonly", "row_limit": 500, "timeout_seconds": 30}` ([ADR-0068](../adr/0068-sql-editor.md)). The whole script runs in one transaction; rollback is the default. Answers every statement's command tag, columns, rows as text cells and rows affected, the server's error with its SQLSTATE, position and line when one happened (as data, not an HTTP error), whether it committed, the duration, and the warnings `check` would give. Scripts with `BEGIN`, `COMMIT`, `ROLLBACK` or `END` need commit mode. Every run goes into the history |
| `POST /_portal/api/db/sql/explain` | `{"sql", "analyze"}` → the plan as `EXPLAIN (FORMAT JSON)` prints it, always rolled back |
| `POST /_portal/api/db/sql/check` | `{"sql"}` → warnings: drops, truncates, deletes and updates without WHERE, dropped columns, type changes, with their lines |
| `GET /_portal/api/db/sql/templates` | Ready-made queries: table sizes, index usage, missing indexes, bloat, connections, locks, slow queries (`pg_stat_statements`), River queues, failed jobs, audit events, migrations, extensions |
| `GET`, `PUT`, `DELETE /_portal/api/db/sql/snippets[/{name}]` | Saved queries as files under `db/queries/<name>.sql`, committed with the app; `PUT` takes `{"sql", "favorite"}`. Favourites are the developer's own, under `.orb/portal/` |
| `GET`, `DELETE /_portal/api/db/sql/history` | The last 500 runs on this machine (`.orb/portal/sql-history.jsonl`), newest first |
| `POST /_portal/api/db/sql/migration` | `{"name", "sql", "apply", "allow_dirty"}`: writes the script as a migration's Up section under `db/migrations`, and applies it when asked |
| `GET /_portal/api/db/migrations` | The files under `db/migrations` with their SQL, whether they have a Down section, and whether and when the database applied them ([ADR-0069](../adr/0069-schema-visualiser-objects-and-migrations.md)) |
| `GET /_portal/api/db/schema-status` | The live schema status ([ADR-0080](../adr/0080-live-schema-status.md), [below](#schema-changes-from-code)), computed now: the same object the `schema` event carries. `{"database": false}` in a Minimal app; 503 `database_unavailable` when PostgreSQL doesn't answer |
| `POST /_portal/api/app/migrate-down`, `migrate-redo` | Roll back the most recent migration (`go run ./cmd/api migrate-down`, or `go run ./cmd/migrate --down` in a v0.1 app), or roll it back and apply it again (`migrate-down` then `migrate`, or `--redo`): the check that its Down works. Development only; 202 through the supervisor |
| `POST /_portal/api/db/ddl/plan` and `apply` | `{"change": {"kind": "add_column", "schema": "public", "table": "projects", "column": {"name": "phone", "type": "text"}}, "name": "add_phone", "allow_dirty": false}`. Kinds: create_table, drop_table, rename_table, add_column, drop_column, rename_column, alter_column, add_foreign_key, add_unique, add_check, drop_constraint, set_primary_key, create_index, drop_index, create_enum, add_enum_value, rename_enum_value, drop_enum, comment, rls, create_extension, drop_extension, create_function, drop_function, create_trigger, drop_trigger, create_view, drop_view (the last six take `definition`, and functions a `signature` such as `add_one(integer)`). `plan` answers the SQL (Up, Down, notes) and the migration file it becomes; `apply` writes the file under `db/migrations` and applies it as `orb dev` does. Managed and system tables are refused (403 `system_table`) |

The app's status:

```json
{"state": "running", "pid": 48213, "addr": "127.0.0.1:8080", "url": "http://127.0.0.1:8080",
 "started_at": "2026-09-16T10:00:01Z", "restarts": 2, "console": true}
```

`state` is `preparing` (services, migrations, seed data), `building`, `running` or `stopped`; `problem` holds the last build or migration failure until the next success (the previous version keeps running meanwhile, as it does in the terminal); `console` says whether the app serves `/_dev/`.

The generators are `job`, `resource`, `migration`, `module`, `middleware`, `add-mail`, `add-storage`, `add-rls` and `add-orgs`; `GET /_portal/api/status` lists the names. Their inputs are the corresponding `orb` command's flags with underscores: `{"name": "CleanupSessions", "schedule": "30 2 * * *", "timeout": "5m", "max_attempts": 8, "queue": "maintenance"}`, `{"name": "Project", "fields": ["name:string:unique", "status:enum(active,archived)"], "scope": "user"}`, `{"name": "add_customer_phone"}`. Defaults and validation are the CLI's; unknown fields are refused. `orb gen … --dry-run` is the same plan printed.

### Tunnel

`orb dev --tunnel quick|named`, or the Tunnel screen, puts the app (only the app) on a public HTTPS address with your own `cloudflared` ([Tunnel](../dev-portal/tunnel.md), [ADR-0086](../adr/0086-dev-portal-tunnel.md)). Development only; the named tunnel's token (`CLOUDFLARE_TUNNEL_TOKEN` or `CLOUDFLARE_TUNNEL_TOKEN_FILE`) is never part of an answer.

| Endpoint | Returns |
|---|---|
| `GET /_portal/api/tunnel` | `status` (`state`: `off`, `starting`, `connected`, `stopping` or `failed`; `mode`, `public_url`, `hostname`, `stable`, `target`, `pid`, `problem`, `restarts`, the last `check`, cloudflared's last 100 lines as `log` with secrets redacted), `cloudflared` (`found`, `path`, `version`), `install` steps per OS, the named tunnel's `hostname` and `hostname_source`, `token_source` or `token_problem`, `app_env`, `allowed`, `target` |
| `POST /_portal/api/tunnel/start` | `{"mode": "quick"\|"named", "hostname": "dev-api.example.com"}`; 202 with the same object. 422 `cloudflared_missing`, `not_development`, `tunnel_token_missing`, `tunnel_token_invalid`, `tunnel_hostname_missing`, `tunnel_hostname_invalid`, `no_app_address`; 400 `invalid_tunnel_mode` |
| `POST /_portal/api/tunnel/stop`, `restart` | 202 with the same object; stopping waits for cloudflared's process group to exit |
| `POST /_portal/api/tunnel/check` | `{"url", "ok", "status", "detail", "checked_at", "latency_ms"}` for `GET <public URL>/livez` compared with the app's own; 409 `tunnel_not_connected` |
| `GET /_portal/api/tunnel/setup` | `changes` (`key`, `current`, `proposed`, `reason`, `optional`) and `set` (the required ones, as `PUT env` takes them), `callbacks` (`provider`, `label`, `url`, `method`, `path`, `where`, `configured`), `routes_known`, `warnings`; 409 without a public URL |
| `PUT /_portal/api/tunnel/settings` | `{"hostname"}`: saves a named tunnel's hostname in `.orb/portal/tunnel.json` |

### Schema changes from code

`orb dev` watches `db/migrations` even with `--no-reload`, and after every migrate run, every change to a migration file and every DDL the SQL editor commits it publishes a `schema` event ([ADR-0080](../adr/0080-live-schema-status.md)), so the portal's Database, Migrations, Schema, Objects and Table Editor screens show the change at once:

```json
{"type": "schema", "time": "2026-09-17T06:00:00Z", "schema": {
  "database": true, "source": "code", "checked_at": "2026-09-17T06:00:00Z",
  "applied": [],
  "pending": [{"file": "20260917000020_x.sql", "version": "20260917000020", "reason": "new"}],
  "edited": [{"file": "20260917000010_invoices.sql", "version": "20260917000010"}],
  "needs_restart": true, "problem": ""}}
```

`source` says what caused it: `startup` (computed when `orb dev` started), `migrate` (a rebuild or restart ran migrations; `applied` lists the files that run applied), `portal` (a portal command ran them: a plan, Apply pending, Roll back, Redo, Reset), `code` (a migration file changed and `orb dev` did not apply it), `sql` (the SQL editor committed a CREATE, ALTER, DROP, TRUNCATE or COMMENT). `pending` lists the files the database hasn't applied; `out_of_order` marks a version lower than the highest applied one, which goose won't apply in order (rename it after the newest). `needs_restart` is true when pending files won't be applied on their own: with `--no-reload`, when the app is stopped or its build failed, or when the last migrate failed (`problem` has the error). With reload on and the app running, a changed file is applied at the next rebuild, as always, and the status that follows has `source: "migrate"` and nothing pending.

When a file changes and won't be applied, `orb dev` also says so in the terminal: `orb: db/migrations changed (20260917000020_x.sql is not applied); restart the app to apply it (Dev Portal → Restart)`. Restart works with `--no-reload` too: it rebuilds, applies the pending files and starts the new build.

`edited` names a trap: a file that was applied and then changed. goose applies a version once and ignores the file afterwards, and PostgreSQL keeps the schema the old content made, so the edit never reaches the database. `orb dev` keeps the SHA-256 of every applied file's content in `.orb/portal/migrations.json` (written after each successful migrate run; files it hasn't seen before are recorded as they are) and prints `orb: 20260917000010_invoices.sql was edited after it was applied; PostgreSQL still has the old version: use Migrations → Redo (development only) or add a new migration`. Redo rolls the last migration back and applies the file as it is now, which is right while the migration is yours and unreleased; anything older or shared gets a new migration. Deleting the record file only loses this check until the next migrate run.

## Screens

| Screen | Shows | From |
|---|---|---|
| Overview | The app's state, uptime, restarts, readiness, health checks, project, links, output as it happens; restart, stop, start | `/_portal/api/status`, `/_portal/api/events`, `/readyz`, `/ops/system` |
| Routes | Every route with its method, path, summary, tags and security, and a request builder that sends through the proxy | `/_dev/routes` |
| Requests | Recent requests and a live tail, with the log records of a request | `/_dev/requests`, `/_dev/requests/stream`, `/_dev/logs` |
| Logs | The local log store ([ADR-0072](../adr/0072-local-log-store.md)): every source (HTTP, auth, jobs, mail, storage, PostgreSQL, app, orb), filters by time, level, user, method, path, status class, duration, request or trace ID and text, a histogram, a live tail, a record's detail, all the records of a request, saved filters, and errors grouped by fingerprint; the store's size and Clear | `/_portal/api/logs…` |
| Modules | What the app wired: libraries, API modules, jobs, settings, flags, permission catalogs | `/_dev/app` |
| Audit | The audit log with filters and statistics | `/ops/audit`, `/ops/audit/stats` |
| Jobs | Definitions, runs, queues; run now, retry, cancel, enable, disable, reschedule, pause and resume; new job by form (kinds custom, HTTP, SQL, email, dispatch), CLI or code through the job generator with a diff preview; the form again for generated jobs, "Ejected: edit in code" once the worker was edited ([ADR-0071](../adr/0071-job-kinds-and-ejection.md)) | `/ops/jobs/…`, `/ops/queues`, `/_portal/api/jobs`, `/_portal/api/generators/job/…` |
| Settings | Runtime settings by group with history; change and reset | `/ops/settings` |
| Mail | The inbox `orb dev` caught (`MAIL_DELIVERY=devmail`): search, HTML (sandboxed), text and source views, verification codes with copy, links, attachments, delete and clear; the app's email previews rendered with sample data and sent to the inbox; the delivery configuration and the suppression list from `/ops/mail` ([ADR-0074](../adr/0074-dev-mail-previews-and-env-editor.md)) | `/_portal/api/mail…`, `/_dev/mail/previews`, `/_dev/mail/preview`, `/ops/mail…` |
| Environment | `.env` against `.env.example`: every key with its description, secrets hidden until revealed, missing keys flagged, add, edit and delete in place, a restart offered after a change; which keys the running app read from `/_dev/config` | `/_portal/api/env…`, `/_dev/config` |
| Generators | Every generator with a form and a diff preview before it writes: `job`, `resource`, `migration`, `module`, `middleware`, `add-mail`, `add-storage`, `add-rls`, and `add-orgs` (its dry run, then its branch workflow) ([ADR-0077](../adr/0077-generators-hub-first-run-and-project-settings.md)) | `/_portal/api/generators/…` |
| Project Settings | The app as the manifest and `.env` describe it: name, module, preset, ports, database, mail delivery, storage driver, CORS origins, logging, docs, each with the key to edit in the Environment screen; API and service-account keys from the ops API; a danger zone: reset the database, clear the log store, the inbox and the SQL history, each confirmed with what it loses | `/_portal/api/project`, `/ops/service-accounts…` |
| Git | The app's repository through your own `git` ([ADR-0076](../adr/0076-git-screen.md), [git guide](git.md)): status, changed files with stage and unstage per file and hunk, the diff viewer, commit, branches (create, switch, delete), fetch, pull, push, merge preview and merge, conflicts opened in the editor, the log graph; every destructive action confirmed with what it loses; never a rewrite or a force push | `/_portal/api/git/…` |
| Storage | The app's file storage ([ADR-0075](../adr/0075-file-storage.md)): the driver and bucket with its status, a file browser with folders, upload, download, delete, move, new folder, image and PDF preview, metadata and signed URLs; a red banner and read-only mode when the store isn't local, until unlocked for the session | `/ops/storage…` |
| Observability | Service health (app, PostgreSQL, mail, Compose services), the API's rates and percentiles per route, the database (connections, cache, sizes, locks, long statements), query performance from `pg_stat_statements` with Explain, Reset and index advice, the machine and the app process, the Go runtime, jobs and sign-ins ([ADR-0073](../adr/0073-observability-screen.md)) | `/_portal/api/health`, `/_portal/api/system`, `/_portal/api/db/stats`, `/_portal/api/db/statements`, `/_portal/api/db/advice`, `/ops/observability/…`, `/ops/system`, `/ops/jobs/overview`, `/ops/audit/stats` |
| Database | Migration state, pool, health checks; apply pending migrations; a banner from the live schema status when files are pending, edited or the last migrate failed | `/_dev/migrations`, `/ops/system`, `/_portal/api/app/migrate`, `/_portal/api/db/schema-status`, the `schema` event |
| SQL Editor | Scripts run in a transaction rolled back by default (or committed, or read-only), results per statement, warnings before running, EXPLAIN, snippets in `db/queries`, history, templates, save as migration | `/_portal/api/db/sql/…` |
| Schema | The tables and their foreign keys as a diagram, by schema; drag to arrange, export as PNG, SVG or Mermaid; click through to the Table Editor | `/_portal/api/db/tables`, `foreign-keys` |
| Objects | Functions, triggers, enums, extensions, indexes and views, each with create and drop as migrations | `/_portal/api/db/functions`, `enums`, `extensions`, `views`, `tables/{schema}/{table}`, `ddl/…` |
| Migrations | Every migration file with its SQL and state; apply pending, roll back, redo; create an empty one; the live status banner (pending, out of order, edited, needs restart) refreshed by every `schema` event | `/_portal/api/db/migrations`, `/_portal/api/db/schema-status`, `/_portal/api/app/migrate…`, `/_portal/api/generators/migration/…` |
| Tunnel | cloudflared's state and install steps, quick or named, start, stop and restart, the public URL and its reachability, cloudflared's output, the `.env` changes for the address applied through the env editor, the URLs to register with Google, Apple, GitHub and Resend ([Tunnel](../dev-portal/tunnel.md)) | `/_portal/api/tunnel…`, the `tunnel` event, `PUT /_portal/api/env` |
| Authentication | Accounts with search and paging; an account's sessions, passkeys, linked providers, second factors and pending codes (with the code from the inbox); create, verify, ban, delete, roles, end sessions, reset MFA; act as a user in the route tester; sign-in providers; rate limiters with reset; [testing each sign-in method](../dev-portal/testing-sign-in.md) (checks, Google, Apple and GitHub round trips, ID tokens, a passkey ceremony, an authenticator code, a test email) without creating accounts | `/ops/auth/users…`, `/ops/auth/providers`, `/ops/auth/rate-limits`, `/_dev/mail`, `/_dev/auth/test/` |
| Table Editor | Every table of every schema, its rows in a grid with filters, sorts and pages; insert, edit, duplicate and delete rows; new tables and columns as migrations shown before they are written; the table's definition; CSV import and export | `/_portal/api/db/…` |

Screens that need `/ops/` show an explanation in an app whose `orb` predates the development operator, and Minimal apps show what they have.

## Building `orb` with the UI

The built UI is committed in `cli/internal/portal/ui/dist`, so `go install gorbital.dev/cli/cmd/orb@latest`, a checkout and the release binaries all serve it; `dist/BUILD` names the gorbital-dashboards commit it was built from. To embed the UI you are working on:

```bash
cd gorbital
scripts/sync-portal.sh            # builds ../gorbital-dashboards/apps/devtools and copies the export into cli/internal/portal/ui/dist
cd cli && go install ./cmd/orb
```

`scripts/sync-portal.sh /path/to/gorbital-dashboards` uses another checkout; `DASHBOARDS_REF=<tag>` checks that ref out first. Commit the copied files when a release should ship them. The script needs Node 22 and pnpm 10. A build whose `dist/` has no `index.html` serves a placeholder page that says how to get the UI.

To work on the UI itself, run it from its source with live reload instead of rebuilding `orb` on every change:

```bash
cd gorbital-dashboards
pnpm install
pnpm devtools dev                 # http://localhost:3101, with orb dev running in your app
```

The development server proxies `/_portal/` to `orb dev` on port 3100 (`ORB_PORTAL_URL` to change it), so the UI at 3101 talks to the real app. Sign in once by opening the link `orb dev` printed, replacing the port with 3101, or open the portal at 3100 first: the cookie is set for `127.0.0.1` and both ports see it.

## Troubleshooting

| Symptom | Cause |
|---|---|
| `port 3100 for the Dev Portal is already in use` | Another program listens there: add `DEV_PORTAL_PORT=3110` to `.env`, pass `--portal-port`, or `--no-portal` |
| `the Dev Portal is for development only, and APP_ENV is production` | The app's `.env` or your environment sets `APP_ENV=production`: use a development `.env`, or pass `--no-portal` |
| The page says the link is from another `orb dev` run | Use the link the running `orb dev` printed; every run has a new token |
| 401 `unauthorized` from the API | No cookie and no bearer token; open the link, or send `Authorization: Bearer <token>` |
| 403 `forbidden` | The request came through another host name, from another machine, or is a write without `X-Orb-Portal`. Use `http://127.0.0.1:3100` or `http://localhost:3100` from this machine |
| 502 `app_unavailable` from `/_portal/app/…` | The app isn't running (see its state and output on the Overview page), or `APP_ADDR` changed and it hasn't restarted |
| The portal shows a placeholder page | This `orb` was built without the UI: run `scripts/sync-portal.sh` and reinstall, or run the UI from source (above) |
| `/_portal/app/_dev/…` answers 404 | The app has no dev console: it runs without `DEV_CONSOLE_TOKEN`, or was created with a development build before the dev console ([upgrade notes](upgrade-notes.md#before-v010-development-builds)) |
