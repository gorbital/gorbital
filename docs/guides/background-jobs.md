# Background jobs guide

`gorbital.dev/modules/jobs` runs background jobs on PostgreSQL with [River](https://riverqueue.com). Decision: [ADR-0033](../adr/0033-background-jobs.md). Admin endpoints: [ops API reference](ops-api.md#job-definitions). Every job a Full app defines, with its default schedule: [jobs reference](../reference/jobs.md).

## Concepts

| Term | Meaning |
|---|---|
| **Job definition** | A named job declared in code, like a serverless function: developers write and deploy the code; operators change its configuration at runtime |
| **Configuration** | `enabled`, `schedule`, `timeout`, `max_attempts`, `queue`, `priority`: code defaults plus operator overrides |
| **Run** | One enqueued job and its attempts |
| **Queue** | A named line of work; each instance works it with a number of workers |
| **Manager** | The admin-panel backend: overrides, history, run now, runs, retry, cancel, queues |

Changing a job's **code** always needs a deploy. Its **configuration** never does.

## Adding a job

Generate a job with the CLI, interactively or with flags ([CLI guide](cli.md#orb-gen-job)):

```bash
orb gen job CleanupSessions                                            # asks the rest
orb gen job CleanupSessions --schedule "0 3 * * *" --timeout 5m --yes  # no questions
```

It creates these files (shown here with the worker filled in):

**`internal/jobs/cleanupsessions/cleanupsessions.go`**

```go
package cleanupsessions

const Name = "cleanup_sessions" // public API: never rename

type Args struct{} // stored as JSON with each job; no personal data

func (Args) Kind() string { return Name }

type Worker struct {
	river.WorkerDefaults[Args]
	sessions SessionPurger
}

func NewWorker(sessions SessionPurger) *Worker { return &Worker{sessions: sessions} }

// Work returns an error to retry; it must respect ctx.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	return w.sessions.PurgeExpired(ctx)
}
```

**`internal/app/job_cleanup_sessions.go`**

```go
func defineCleanupSessionsJob(defs *jobs.Definitions, deps jobDeps) {
	jobs.Define(defs, jobs.Definition[cleanupsessions.Args]{
		Name:        cleanupsessions.Name,
		Description: "Deletes expired sessions.",
		Worker:      cleanupsessions.NewWorker(deps.sessions),
		NewArgs:     func() cleanupsessions.Args { return cleanupsessions.Args{} },
		Enabled:     true,
		Schedule:    "0 3 * * *",
		Timeout:     5 * time.Minute,
		MaxAttempts: 5,
	})
}
```

and adds `defineCleanupSessionsJob(defs, deps)` below `//orb:anchor jobs` in `internal/app/jobs.go`. Without the CLI, create the same files by hand.

`Define` panics at startup when the name isn't lowercase snake_case, `Args.Kind()` doesn't equal the name, the worker or `NewArgs` is missing, or the defaults are out of bounds. Zero `Timeout`, `MaxAttempts`, `Queue` and `Priority` become 1 minute, 25, `default` and 1.

## Jobs from the portal

The Dev Portal's Jobs screen ([Dev Portal guide](dev-portal.md)) makes a job three ways: a form, the `orb gen job` command to copy, or the custom kind with the file to open. The form asks what the job does and `orb gen job --kind` renders it as ordinary Go ([ADR-0071](../adr/0071-job-kinds-and-ejection.md)):

| Kind | `Work` | Needs from `jobDeps` |
|---|---|---|
| `custom` | The skeleton above: logs a line, for you to write | `logger` |
| `http` | Sends `Method URL Body` with the app's HTTP client (30 s timeout); a failed request or a non-2xx answer is an error, so the job is retried | `httpClient` |
| `sql` | Runs `Statement` on the app's pool | `pool` |
| `email` | Sends `To`, `Subject`, `Text` through the app's mailer (the `mail.*` settings and suppressions apply) with `<name>-<job ID>` as idempotency key, so a retry never sends twice | `mailer` |
| `dispatch` | Starts the job named `Target`, as `POST /ops/jobs/definitions/{name}/run` does | `runJob` |

The definition file carries a marker above `define<Ident>Job`:

```go
//orb:job {"kind":"http","http_method":"POST","http_url":"https://example.com/hook","worker":"sha256:…"}
```

The portal reads it back to show the job as a form again. `worker` is the SHA-256 of the worker file as generated: once you edit that file, the hashes differ and the job is **ejected**: the portal shows it as a custom job edited in code and never offers to overwrite it. That is the intended path when a kind stops fitting (move the constants into `Args`, add dependencies, change the logic). Hand-written jobs have no marker and are custom jobs from the start.

## Configuration

| Field | Allowed | A change applies to |
|---|---|---|
| `enabled` | true/false | Disabled: the schedule stops and "run now" is refused; jobs enqueued by code still run |
| `schedule` | 5-field cron in UTC (`0 3 * * *`), descriptors (`@daily`, `@every 15m`), or empty for on-demand; at most once a minute | The next schedule, within seconds, on the leader |
| `timeout` | 1s to 24h | Attempts starting after the change (the worker's own `Timeout` method is ignored) |
| `max_attempts` | 1 to 100 | Jobs enqueued after the change |
| `queue` | A queue some worker runs | Jobs enqueued after the change |
| `priority` | 1 (highest) to 4 | Jobs enqueued after the change |

Queue, priority and max attempts are applied to every job of a defined kind, whoever enqueues it. Scheduled jobs run once across all instances because only River's elected leader inserts them. The admin panel's next run time is approximate: the leader keeps the real timer in memory.

## Wiring

```go
defs := jobs.NewDefinitions()
defineJobs(defs, jobDeps{logger: logger})

client, err := jobs.New(pool, nil,
	jobs.WithQueues(map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 10}}),
	jobs.WithDefinitions(defs),
	jobs.WithLogger(logger),
	jobs.WithTracerProvider(tel.TracerProvider()),
)
manager, err := jobs.NewManager(ctx, pool, client, recorder, jobs.WithManagerLogger(logger))

runners := []app.Runner{server, client, manager}
```

| Option | Default | Notes |
|---|---|---|
| `WithQueues(map)` | none (insert-only client) | An API process can enqueue while a separate worker process works jobs |
| `WithDefinitions(defs)` | none | Registers workers, live configuration and schedules; use the same definitions in every process |
| `WithStopTimeout(d)` | 20s | On shutdown, running jobs get this long before their contexts are cancelled; keep below the app's 25s |
| `WithRetention(completed, cancelled, discarded)` | 1h, 24h, 7d | Arguments may contain personal data, so completed jobs are kept briefly |
| `WithLogger`, `WithTracerProvider`, `WithPropagator` | discard, global, global | |
| `WithJobTimeout`, `WithMaxAttempts` | River's (1m, 25) | For jobs without a definition |

## Enqueueing from code

```go
res, err := client.Insert(ctx, cleanupsessions.Args{}, nil)

err = postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
	// ... write rows with tx ...
	_, err := client.InsertTx(ctx, tx, welcome.Args{UserID: id}, nil) // only runs if tx commits
	return err
})
```

## What a worker sees

| In the context | Value |
|---|---|
| `requestid.From(ctx)` | The request ID that enqueued the job |
| Trace | A consumer span `job <kind>`, child of the enqueuing trace |
| `actor.From(ctx)` | `actor.System("jobs")` with the original org ID and **no permissions** |
| `jobs.OnBehalfOf(ctx)` | The actor who enqueued it (kind, ID, label, org), for audit metadata |

Authorise work when enqueuing: a job never runs with a user's permissions.

## Failures, retries and shutdown

- Return an error to retry with River's backoff; `river.JobCancel(err)` stops retrying; `river.JobSnooze(d)` retries later without using an attempt.
- Each failed attempt is logged once: a warning while attempts remain, an error on the last one. Panics are logged with their stack.
- On shutdown no new jobs are fetched; running jobs finish or are cancelled after the stop timeout.

## Email

```go
workers := river.NewWorkers()
_ = jobs.AddMailWorker(workers, sender) // works "gorbital.mail.send" jobs; sender is Resend, SMTP or Mailpit
client, err := jobs.New(pool, workers, jobs.WithQueues(jobs.DefaultQueues()))
mailer := mail.WithDefaults(jobs.AsyncSender(client), senderSettings) // validates, fills the sender, enqueues
```

Delivery is retried up to 8 times; each job's ID becomes the provider idempotency key (`job-<id>`) unless the message sets one, so retries never send twice. A send that fails with `mail.ErrRejected` (an unverified domain, a refused address) is cancelled at once instead of retried, and the run keeps the reason. Email addresses in the error are replaced with `[email]` (`mail.RedactAddresses`) before it is stored with the run or logged, since providers often quote the recipient; River's own log lines through `WithLogger` are redacted the same way. Choosing the provider and the sender settings: [email guide](email.md).

## Managing jobs in Go

| Method | Purpose |
|---|---|
| `Definitions`, `Definition`, `Scheduled` | Views with effective and default configuration, version, next run and last run |
| `Update(ctx, name, ConfigPatch, Change)` | Change fields; setting a field to its default removes that override |
| `Reset(ctx, name, Change)` | Back to code defaults |
| `History(ctx, name, before, limit)` | Changes, newest first |
| `RunNow(ctx, name)` | Enqueue an enabled job now; `ErrRunLimited` while a run is queued or running, or within `MinScheduleInterval` (1 minute) of its last run |
| `Jobs(ctx, JobFilter)`, `Job(ctx, id)` | Runs without arguments, cursor pagination |
| `Retry`, `Cancel` | Control one run. `Retry` accepts runs waiting to retry, discarded or cancelled (`ErrJobNotRetryable` otherwise, so a completed run never runs twice) of enabled definitions (`ErrDefinitionDisabled`) |
| `Queues`, `PauseQueueWithReason`, `ResumeQueueWithReason` | Queues across all instances; pausing needs a reason, recorded in the audit event. `PauseQueue` is deprecated and returns `ErrReasonRequired` |

Rules: writes need an authenticated actor; `Update` and `Reset` need the current version; disabling a job, changing an enabled job's schedule, or changing its timeout, max attempts or queue needs a reason (each can stop the job working while it looks enabled); moving a job to a queue requires that queue to be active. Audit actions: `jobs.definition.changed`, `jobs.definition.run_requested`, `jobs.run.retried`, `jobs.run.cancelled`, `jobs.queue.paused`, `jobs.queue.resumed`.

## Migrations and storage

| Tables | Created by |
|---|---|
| River's (`river_job`, `river_queue`, `river_leader`, `river_migration`, …) | `jobs.Migrate(ctx, pool)` in `cmd/migrate`, after goose; `jobs.MigrationsPending` reports gaps |
| `jobs_definitions`, `jobs_definition_history` | goose migration from `jobs.Migrations`, copied into `db/migrations` |

## Testing

- Worker logic: call `Work` directly with a `&river.Job[Args]{JobRow: &rivertype.JobRow{ID: 1}}`.
- Integration: `pgtest.New(t, pgtest.WithMigrations(jobs.Migrations))`, then `jobs.Migrate`, then run the client; wait with `client.River().Subscribe(river.EventKindJobCompleted)`.
