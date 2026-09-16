# ADR-0071: Jobs from the portal: kinds, markers and ejection

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0021, ADR-0033, ADR-0066

## Context

Phase 6 of the [Dev Portal roadmap](../dev-portal-roadmap.md) is the Jobs screen: make a job from a form (name, schedule, retries and what it does), see it as a form again later, and know when it was edited by hand. `orb gen job` already writes a job package, its test and a definition file, and the portal plans and applies generators through `genplan` ([ADR-0066](0066-dev-portal.md)). Every generated job is a `Work` method that logs a line and waits to be written; there is no way to say what the job does, and nothing tells a generated job from a hand-written one.

## Options

### What a form-made job can do

| Option | Verdict |
|---|---|
| Only custom jobs: the form writes the skeleton, the developer writes `Work` | Rejected: the common jobs (call a webhook, run a cleanup statement, send a reminder, chain a job) would still need code |
| An interpreted job (the form's answers stored in the database, run by one generic worker) | Rejected: it hides the job from code review, tests and `orb upgrade`, and it needs a runtime the app doesn't have |
| **Kinds rendered as ordinary Go: `custom` (the skeleton), `http` (a request that must answer 2xx), `sql` (one statement on the app's pool), `email` (a message through the app's mailer, the job ID as idempotency key), `dispatch` (starts another job by name). Each kind is a different `Work`, with the request, statement, message or target as constants at the top of the file** | **Chosen**: the file is what runs, reads like the hand-written jobs, and the developer edits it when the kind stops fitting |

### What the generated jobs need from the app

| Option | Verdict |
|---|---|
| Each job builds its own client or pool | Rejected: a second pool per job, a mailer without the runtime settings |
| **`jobDeps` in both Full apps gains `pool` (`*pgxpool.Pool`), `mailer` (`mail.Sender`, the app's, so `mail.*` settings and suppressions apply), `httpClient` (30 s timeout) and `runJob` (the job manager's `RunNow`); the definition passes what its kind needs** | **Chosen**: the same wiring the built-in jobs use |

### Telling a generated job from a hand-written one

| Option | Verdict |
|---|---|
| A record in `.orb/portal` | Rejected: it isn't in git, so a teammate's checkout shows the job as hand-written; it drifts when the file is renamed |
| A `gorbital.lock` entry | Rejected: the lock is for `orb upgrade` and hashes every file the templates wrote; jobs are the app's own code |
| **A marker comment on the definition: `//orb:job {json}` with the kind, the kind's fields and `worker`, the SHA-256 of the worker file as generated. The portal reads the marker back to fill the form; when the worker file no longer hashes to the marker's value the job is ejected: shown as a custom job edited in code, never overwritten by the form** | **Chosen**: the marker travels with the code in git, and an edit anywhere in the worker ejects, so the form can't undo a change |

Hashing the worker rather than diffing it keeps the rule simple: a formatting change ejects too, which is fine, because the form's only purpose is to write the file once.

## Decision

| Piece | Decision |
|---|---|
| `orb gen job` | `--kind custom\|http\|sql\|email\|dispatch` (default custom) with `--method`, `--url`, `--body` (JSON), `--sql`, `--to`, `--subject`, `--text`, `--dispatch`; a flag of another kind is refused. The prompts ask for the kind and its fields |
| Templates | `job/job.go.tmpl` renders one `Work` per kind; `job/job_test.go.tmpl` tests the request against a local server (http), the message through a `mail.SenderFunc` (email), the started name (dispatch), the statement's presence (sql); `job/definition.go.tmpl` carries the marker and passes the deps |
| Marker | `//orb:job {"kind":…,"http_method":…,"http_url":…,"http_body":…,"sql":…,"email_to":…,"email_subject":…,"email_text":…,"dispatch_target":…,"worker":"sha256:…"}` above `define<Ident>Job`; `recipes.ParseJobMarker` and `recipes.WorkerHash` |
| Golden apps | `jobDeps.pool`, `mailer`, `httpClient`, `runJob`; `App.mailer` kept for the closure (the mailer is built after the jobs client); the heartbeat definition carries a marker |
| Portal | `GET /_portal/api/jobs`: `{jobs: [{name, ident, package, definition, worker, generated, ejected, kind, form}]}` from `internal/app/job_*.go`; the Jobs screen joins it with `/ops/jobs/definitions` by name |
| Screen | New job three ways (form → `generators/job/plan` and `apply` with the diff, then restart; CLI command to copy; code: the custom kind and the file to open), the visual view for generated jobs, "Ejected: edit in code" once the worker changed, and the existing runs, queues and scheduled views |

## Why

- Generated jobs are code: reviewed, tested (`TestWorker` per kind), upgraded and deployed like the rest of the app.
- The marker is the smallest record that survives git and renames of the app, and a hash is the simplest ejection rule that can't be wrong.
- The deps are the app's own: the email job respects suppressions and the runtime sender settings, the SQL job uses the app's pool.

## Trade-offs

- A generated job's values (URL, statement, message) are constants: to vary them per run, the developer moves them into `Args` and ejects, which is the intended path.
- The SQL kind runs one statement with no parameters; anything more is a custom job.
- The marker's JSON is machine-written; editing it by hand doesn't eject (only the worker counts), so a wrong edit shows a wrong form until the worker changes.

## Consequences

- Upgrade notes: existing Full apps get the four `jobDeps` fields and `App.mailer` through `orb upgrade`; hand-written jobs are unaffected; their definitions have no marker and show as custom.
- Guides: [background jobs](../guides/background-jobs.md) ("Jobs from the portal"), [CLI](../guides/cli.md) (`orb gen job` flags), [Dev Portal](../guides/dev-portal.md).
- [ADR-0066](0066-dev-portal.md) is amended: the portal's API gains `GET /_portal/api/jobs`.

## Implementation notes (2026-09-16)

`TestGenJobKinds`, `TestGenJobKindValidation` and `TestJobSources` in `cli/internal/cli`; `TestJobsListsTheAppsJobs` in `cli/internal/portal`; `TestJobMatchesGoldenApp` renders the heartbeat job with its marker.
