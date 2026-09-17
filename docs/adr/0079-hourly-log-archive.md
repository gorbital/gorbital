# ADR-0079: Hourly log archive

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0007, ADR-0072, ADR-0075

## Context

An app's log records leave the process on standard output and are kept by whatever collects them: `orb dev`'s log store in development ([ADR-0072](0072-local-log-store.md)), a log pipeline in production, or nothing. Teams without a pipeline asked for the cheapest durable copy: the records of each hour as a file in the storage bucket the app already has ([ADR-0075](0075-file-storage.md)), listed by `/ops/storage` and the Dev Portal's Storage screen, switched on by an operator when they want it and off when they don't.

## Options

### Where the code lives

| Option | Verdict |
|---|---|
| A job that reads the log store | Rejected: the log store belongs to `orb dev` and exists only in development; production has no table of records to read |
| A second logger writing to the bucket per record | Rejected: one object per record is unusable and slow; a `Put` on the logging path would block the app |
| **A library package, `gorbital.dev/modules/storage/logarchive`: an `slog.Handler` that joins the app's logger as a tee ([telemetry.WithLogTee](../../modules/telemetry), which now takes several handlers) and copies each record as a JSON line into a spool file for the current hour; an uploader goroutine that gzips finished hours and stores them through the app's `storage.Store`** | **Chosen**: the logging path only appends to a buffered file; the bucket sees one object per hour |

### What turns it on

| Option | Verdict |
|---|---|
| An environment variable | Rejected: a redeploy to keep logs during an incident, and to stop keeping them after |
| **A runtime setting, `logs.archive.enabled` (bool, default `false`, reason required), read by the handler as a live `config.Value[bool]`: on collects from the next record, off stops collecting and uploads what was collected as a partial hour** | **Chosen** |

### Keys

| Option | Verdict |
|---|---|
| One key per hour, `logs/<service>/<YYYY>/<MM>/<DD>/<HH>.jsonl.gz` | Rejected as the only form: two instances of the same service would overwrite each other's hour |
| **The hour's key with the instance after it when the app names one (the Full apps pass the host name): `logs/<service>/<YYYY>/<MM>/<DD>/<HH>.<instance>.jsonl.gz`; a partial hour (shutdown, or the setting turned off) adds `.partial-<unix>` after the instance; `application/gzip`, with the service in the object's metadata** | **Chosen**: a day's folder lists every instance's hours side by side |

### Never losing lines, never blocking

The spool file is opened for appending, so an instance restarting within the hour continues it. A failed upload is logged and the file stays on disk; the uploader retries at its next tick (every minute) and, at start, uploads whatever a previous run left, including a crashed one's partial hour. The spool is buffered (64 KiB, flushed every tick and at every upload), so a crash can lose the last minute's lines from the archive; standard output still has them. An hour with no records leaves no object.

## Decision

| Piece | Decision |
|---|---|
| Package | `logarchive.New(dir, service, …)` with `WithLevel` (default info), `WithInstance`, `WithInterval` (default a minute), `WithClock`; `(*Archive).Handler()` (the tee), `Bind(store, enabled, logger)` (starts the uploader; a nil store logs one warning when the setting is on and collects nothing), `Close(ctx)` (uploads the partial hour), `Status()` (collecting, pending files, last key, last upload, last error); `DefaultDir` (`.orb/logs`), `KeyPrefix` (`logs/`), `ContentType` |
| App | Setting `logs.archive.enabled` (group `logs`, reason required); `LOG_ARCHIVE_DIR` (default `.orb/logs`, created on demand, mode 0700, files 0600); `internal/app/logarchive.go` builds the archive with the app's log level and host name; `newBase` adds the handler as a second tee and registers `Close` on the cleanup stack after telemetry, so the partial hour is stored while the logger still works; `build` binds it to the store and the setting |
| Objects | `logs/<service>/<YYYY>/<MM>/<DD>/<HH>[.<instance>][.partial-<unix>[-n]].jsonl.gz` in UTC, one JSON record per line as `APP_LOG_FORMAT=json` writes them, with `service` on every record; listed by `/ops/storage/objects?prefix=logs/` and the Storage screen, downloaded like any object |
| Telemetry | `WithLogTee` given more than once sends every record to every handler |

## Why

- The bucket is already there, credentials and all; an hour of JSON Lines gzipped is a few hundred kilobytes for most apps and greps fine.
- The setting is where operators already are (`/ops/settings`, the portal's Settings screen), takes effect within seconds on every instance, and its history says who turned it on and why.
- Files first, upload later: logging stays a file append; the object store's latency and failures never reach a request.

## Trade-offs

- It is an archive, not a search: the Logs screen reads `orb dev`'s store, not the bucket. A pipeline (Loki, CloudWatch, Datadog) remains the answer for querying production logs.
- Up to a minute of records can be lost from the archive by a crash (the buffer), and the top of the hour is uploaded within a minute of it, not at it.
- Every instance writes its own objects; a bucket lifecycle rule, not the app, expires them. Log records can hold IDs and addresses, so the setting requires a reason and the description says so.
- With the `local` driver the archive is a copy of `.orb/portal/logs` in another folder; harmless, and how the feature is tried.
- The default directory is relative to the working directory: inside the container in the generated image (`/home/nonroot/.orb/logs`), so a crash followed by a new container loses the hour on disk unless `LOG_ARCHIVE_DIR` names a volume. A directory that can't be written is logged, not fatal: the setting can be turned on at any time, and startup must not depend on it.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)): log records, which never carry emails, tokens or secrets by the logging rules, are stored under `logs/` behind the same `ops.storage.*` permissions as every object; the spool directory is private to the process's user.
- Upgrade notes: Full apps gain `internal/app/logarchive.go`, the setting, the config field, the `.env.example` line and a second `WithLogTee`; nothing changes until the setting is on.
- Guides: [observability](../guides/observability.md) ("The hourly log archive"), [storage](../guides/storage.md), [environment variables](../guides/environment-variables.md), [ops API](../guides/ops-api.md), the [settings reference](../reference/settings.md).

## Implementation notes (2026-09-17)

`modules/storage/logarchive` tests with a fake clock and store: off collects nothing, rotation at the hour with the JSON lines checked, the partial hour at `Close`, toggling off and on within an hour, a failed upload kept and retried, leftovers of a previous run, no store warns once, the real uploader loop, keys with and without an instance; `TestLogTee` in `modules/telemetry` for several tees; `TestLogArchive` in both Full apps (the setting through `/ops/settings`, a request's record found in the `local` store's object after `Close`).
