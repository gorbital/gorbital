# ADR-0072: The Dev Portal's log store

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0018, ADR-0065, ADR-0066

## Context

Phase 7 of the [Dev Portal roadmap](../dev-portal-roadmap.md) is the Logs screen: every source (HTTP, auth, jobs, mail, storage, PostgreSQL, the app, `orb dev` itself), filters by time, level, user, method, path, status class, duration, request or trace ID and text, a histogram, a live tail, "all logs for this request", saved filters, and errors grouped by fingerprint. Today the app keeps its last 500 records in memory for the dev console (`/_dev/logs`, [ADR-0065](0065-local-dev-console-apis.md)), which restarts empty and can't answer "what happened before the crash"; `orb dev` keeps the last 2,000 output lines in memory for the portal's console. Request records carry the route but not the path or the user, and no record says which part of the app wrote it.

## Options

### Where records live

| Option | Verdict |
|---|---|
| The app's dev console buffer, made bigger | Rejected: lost on every restart, which is when the records matter most; the app shouldn't grow a database of its own logs |
| SQLite under `.orb` | Rejected as in [ADR-0068](0068-sql-editor.md): a C or a large pure-Go dependency in `orb` for a development store |
| **JSON Lines segments under `.orb/portal/logs`, written by `orb dev` from the app's output, rotated at 8 MiB and bounded to 64 MiB, the oldest segment dropped first; queries scan the segments newest first** | **Chosen**: no dependency, survives restarts of the app and of `orb dev`, bounded, and a `grep`-able file when the portal isn't enough |

Full-text search is a case-insensitive substring scan; at development sizes (tens of megabytes) a query takes well under a second, and an index isn't worth its complexity.

### Where the structure comes from

| Option | Verdict |
|---|---|
| Parse the text log format | Rejected as the only way: `key=value` with Go quoting parses, but values with spaces and groups are lossy |
| **The app logs JSON when `orb dev` runs it (`APP_LOG_FORMAT=json`, a new setting, empty by default: JSON in production, text elsewhere); `orb dev` renders each JSON line back to text for the terminal and stores the structure. Text and plain lines are still parsed or kept raw, so an app that insists on text works** | **Chosen**: the same records the app writes in production, with every attribute |

### What a record says about its source

| Option | Verdict |
|---|---|
| Guess from the message | Rejected as the only way: fragile |
| **A `source` attribute set where the logger is handed to a part of the app: `http` by `httpx.AccessLog`, `auth`, `jobs` and `mail` by the golden apps' wiring (`logger.With("source", …)`), `postgres` for the container's log, `orb` for `orb dev`'s messages; records without one are `app`, after a few shape rules (a `job_id` means jobs)** | **Chosen**: explicit where it is cheap, a fallback for the rest |

### The user on request records

The access log runs before authentication, so the authenticated user is set on a context it never sees. `httpx.AccessLog` now puts an `AccessNote` in the request's context; `auth.Middleware` adds `user_id` to it, and the note's attributes join the record. Any handler can add attributes the same way (`httpx.AccessNoteFrom(ctx).Add(...)`).

## Decision

| Piece | Decision |
|---|---|
| Records | `httpx.AccessLog` adds `source=http`, `path` (no query string) and the note's attributes (`user_id`); the golden apps tag the jobs, auth and mail loggers with `source` |
| App setting | `APP_LOG_FORMAT` (`json`, `text`, or empty for the old rule) in every preset; `orb dev` sets `json` unless `.env` chose, and prints text |
| Store | `cli/internal/portal.LogStore`: `.orb/portal/logs/logs-<first ID>.jsonl`; records `{id, time, source, level, message, attrs[], raw}`; `Ingest` parses JSON, text and PostgreSQL lines; `Query` (filters, `before`/`after` paging by ID), `Histogram`, `Errors` (fingerprint = the message with numbers, IDs, hex and quoted values replaced + the first line of `stack`/`error`), `Stats`, `Clear`, saved filters in `.orb/portal/log-filters.json`, subscriptions for the tail |
| Sources | Every hub line (the app's output, `orb dev`'s messages) and, with services on, `docker compose logs -f postgres` |
| Endpoints | `GET /_portal/api/logs` (`from`, `to`, `level`, `min_level`, `source`, `user`, `method`, `path`, `status_class`, `status`, `min_duration_ms`, `request_id`, `trace_id`, `q`, `before`, `after`, `limit`), `logs/histogram` (`bucket`), `logs/stream` (SSE, same filters, `after` to close the gap), `logs/errors`, `logs/request/{id}`, `logs/stats`, `DELETE logs`, `GET|PUT logs/filters`, `DELETE logs/filters/{name}` |
| `orb dev` | Command output (migrations, seeds, Docker) reaches the portal's console and the store too, so a failed migration's error is readable there |
| The portal | The Logs screen on the store: sources, filters, histogram, live tail, a record's detail, "all logs for this request", saved filters, the errors view; retention shown and cleared from Project Settings |

## Why

- The store belongs to `orb dev`, not the app: the app's own logging is unchanged in production, and the records outlive the process that wrote them.
- JSON from the app is the cheapest structure: nothing to parse wrong, and what the app logs is what the portal shows.
- Files under `.orb` need no service, are bounded, and are readable with the tools already on the machine.

## Trade-offs

- Postgres records come from Docker; without services (`--no-services`) there is no `postgres` source.
- The terminal shows a rendering of the JSON line, close to slog's text format but not byte-identical (times are local, values quoted as slog would).
- The fingerprint is a heuristic: two different problems with the same message shape group together; a rewritten message splits a group.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)): the store holds whatever the app logs, under `.orb` (gitignored) with mode 0600; the portal's guard applies to every log endpoint; `DELETE logs` clears it.
- Upgrade notes: `APP_LOG_FORMAT` in `.env.example`, the `source` tags in `app.go` and the config field come with `orb upgrade`; hand-rolled middleware chains get `path`, `source` and the note for free from `httpx.AccessLog`.
- Guides: [observability](../guides/observability.md) ("The local log store"), [request lifecycle](../guides/request-lifecycle.md), [environment variables](../guides/environment-variables.md), [Dev Portal](../guides/dev-portal.md).

## Implementation notes (2026-09-16)

`cli/internal/portal/logstore_test.go` (ingestion of JSON, text, raw and PostgreSQL lines; filters and paging; reopening keeps IDs; rotation and the size bound; error groups; saved filters; subscriptions; the text renderer), `TestLogEndpoints` in `portal_test.go` (every endpoint and the live tail), `httpx` tests for the note.
