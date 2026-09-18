# 13. Work that happens without a request

Everything Plateful has done so far started with somebody pressing a button. A customer places an order, a kitchen accepts it, a courier says they arrived. This chapter is about the other kind of work: nobody asked, the clock did.

The job we're adding is a sweep. A restaurant accepts an order and then the evening gets away from them — the order sits in the kitchen, the customer watches a screen that says "preparing", and nobody notices. Every five minutes, Plateful looks for orders a kitchen has been holding too long and tells that restaurant about them.

By the end you'll have written a job definition, understood the two traps that catch everybody the first time, seen the three answers a worker can give the job system, and know what an operator can change at three in the morning without calling you.

Depth on the job system itself — wiring, queues, email delivery, migrations, the Go API for managing runs — is in the [background jobs guide](../guides/background-jobs.md). This chapter is about putting one job in a module and living with it.

## 1. Declare the job in the module

**What we're doing.** Adding a named, scheduled job to the orders module, in the `Jobs` field of the `gorbital.Module` that [chapter 12](12-your-own-function.md) left us with.

**Why.** "Which orders are late?" is a question about the whole platform, asked on a clock. No request can ask it: a request belongs to one caller in one organisation, and this reads across every restaurant at once. It also must run once across every instance of the app, not once per instance, which is not something a `time.Ticker` in `main.go` can promise.

**What the framework already gives us.** All of the hard parts. `gorbital.dev/modules/jobs` runs jobs on PostgreSQL through [River](https://riverqueue.com): a queue, a leader election so a scheduled job fires once across the fleet, retries with backoff, per-attempt timeouts, a history of runs, and `/ops` endpoints where an operator changes the schedule without a deploy. `gorbital.New` builds the client, registers the workers and runs them; the module never sees any of that.

**What we build ourselves.** A name, a description, an arguments type, a worker, and the defaults the job starts life with.

**How.** `Module.Jobs` is `func(defs *jobs.Definitions, d gorbital.Deps)`, and `gorbital.New` calls it once per module at startup:

<!-- include examples/apps/plateful/internal/modules/orders/module.go#jobs -->

**What just happened.** The app now has a job called `orders_late_sweep`. It is enabled, it fires every five minutes, each attempt gets two minutes, and it runs on the `default` queue at priority 3. It appears in `GET /ops/jobs/definitions` with that configuration, it can be run on demand from `/ops`, and its runs and their errors are listed in `GET /ops/jobs/runs?kind=orders_late_sweep`.

`jobs.Define` is strict, and it is strict at startup rather than at three in the morning. It refuses a name that isn't lowercase `snake_case`, arguments whose `Kind()` doesn't equal the name, a missing worker or `NewArgs`, and defaults outside the allowed bounds. Left at zero, `Timeout`, `MaxAttempts`, `Queue` and `Priority` become one minute, 25, `default` and 1.

The name is public API in the same way an error code is. Renaming `orders_late_sweep` orphans its operator overrides and its run history, both of which are keyed by the name; nothing will tell you, because a renamed job is indistinguishable from a new one.

## 2. The two traps

Both come from **when** `Jobs` runs, and both bite silently.

### `Deps.Jobs` is nil inside `Module.Jobs`

`gorbital.New` builds the job client *from* the definitions, so it calls every module's `Jobs` before the client exists. The `d gorbital.Deps` handed to `Jobs` therefore has a nil `Jobs` field — and a nil `*jobs.Client` doesn't complain when you store it, only when something calls it, which is minutes later inside a worker.

> **Don't do this.** Capture `d.Jobs` in the worker and use it in `Work`:
>
> ```go
> Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
>     worker := usecase.NewLateSweepWorker(svc, d.Jobs, d.Logger) // d.Jobs is nil, always
>     // …
> },
> ```

**Do this instead.** Keep from `d` only the things that already exist — the pool, the logger, a service you build here — and take the job client from the context when the job actually runs:

```go
client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
if err != nil {
    return fmt.Errorf("no job client in this context: %w", err)
}
_, err = client.Insert(ctx, someArgs, nil)
```

River puts its own client in the context it works a job with. Use `ClientFromContextSafely`, not `ClientFromContext`: the safe one returns an error where the other panics, which is the difference between a test that fails with a message and a test that takes the whole process down.

In the definition above, that is what `notifications.FromWorker` is — the orders module hands its worker a *function* rather than a client, and the function finds the client at work time. [Chapter 17](17-extending-the-framework.md) builds the module on the other end of it.

### A worker's own `Timeout()` is ignored

River lets a worker declare `func (w *Worker) Timeout(*river.Job[Args]) time.Duration`. In a gorbital app, writing one is wasted effort with a misleading result: the definition's `Timeout` is what applies, because the timeout is operator-editable and the definition is where an operator's change lands. A worker that says `Timeout: 10 * time.Minute` and a definition that says `2 * time.Minute` gives you two minutes, and no error anywhere says why.

Put the number in the definition. If it needs to change, it changes in `/ops` and not in a deploy.

## 3. The arguments and the worker

**What we're doing.** Writing the two types the definition names.

**Why.** Arguments are how a job is identified and re-created. River stores them as JSON with the job row, so they are also a place personal data leaks into a queue table that outlives the request.

**What the framework already gives us.** The `river.JobArgs` interface (one method, `Kind()`), `river.WorkerDefaults[T]` so a worker only has to write `Work`, and the plumbing that decodes a stored row back into your type.

**What we build ourselves.** The name constant, the arguments, and the work.

**How.** The sweep takes no arguments at all — it reads the whole platform, and the time it compares against is the moment it runs:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/late_sweep.go#late-sweep-args -->

And the work itself:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/late_sweep.go#late-sweep-work -->

**What just happened.** Notice how little is in `Work`. It gets the time, calls a use case, and asks for one notification per restaurant. The interesting logic — which orders count as late, how they are grouped, what the message says — is in `Service.LateOrders`, an ordinary use case beside the ones the API serves, callable from a test without a job system anywhere in sight. A worker is an adapter. It belongs in the same category as an HTTP handler: it translates one kind of trigger into a use case call and translates the result back.

`LateOrders` reads `orders.late_after`, a runtime setting, *when the job runs*. An operator changes it in `/ops/settings` and the next sweep five minutes later uses the new value. That is [chapter 14](14-settings-and-feature-flags.md).

## 4. Schedules

`Schedule` is a string, and there are three shapes of it:

| Shape | Example | Means |
|---|---|---|
| 5-field cron | `"0 3 * * *"` | Every day at 03:00 |
| Descriptor | `"@daily"`, `"@every 5m"` | What it says |
| Empty | `""` | On demand only: nothing fires it but code, `/ops` or another job |

Three rules are worth knowing before you write one.

**Always UTC.** A cron expression is evaluated in UTC on every instance, whatever the server's time zone is. `"0 3 * * *"` is 03:00 UTC, which is 04:00 in London in summer and 03:00 in winter. If a job must run at a local hour, that is a decision to make explicitly, not one to inherit from whichever machine happened to schedule it.

**At most once a minute.** `MinScheduleInterval` is one minute and the floor is enforced: `@every 30s` is refused when the definition is declared, which means at startup, which means the app doesn't start. A 5-field cron expression can't express anything finer than a minute anyway.

**Once across the fleet.** Every instance registers the same schedules, but only River's elected leader inserts the job, so ten instances don't produce ten sweeps. The "next run" time `/ops` reports is approximate, because the real timer lives in the leader's memory.

## 5. Retries, and the three answers a worker can give

**What we're doing.** Deciding what happens when a job fails.

**Why.** Most background work fails sometimes and succeeds on the next try — a database blip, a busy third party, a deploy in the middle of an attempt. A few failures will never succeed, however many times you try. Telling those apart is the worker's job, and it is the only thing about failure the worker decides.

**What the framework already gives us.** The retry loop, its backoff, the attempt counter, the per-attempt timeout, the stored error history, and the log line per failed attempt (a warning while attempts remain, an error on the last one, with the stack for a panic). There is nothing for you to write.

**What we build ourselves.** One `return` statement, chosen from three:

| `Work` returns | The job system does | Use it for |
|---|---|---|
| `nil` | Marks the run completed | Success, and "there was nothing to do" |
| An ordinary `error` | Retries with backoff, up to `MaxAttempts`, then discards the run | A failure that might not happen next time: a refused connection, a 500, a timeout |
| `river.JobCancel(err)` | Stops now; the run is cancelled, not discarded | A failure that will never come right: a row that has been deleted, arguments that can't be valid |
| `river.JobSnooze(d)` | Schedules the run `d` from now **without spending an attempt** | "Not yet": a dependency that isn't ready, a window that hasn't opened |

The distinction between *discarded* and *cancelled* matters more than it looks. A discarded run sits in the operator's list of things that went wrong and needs a human to decide whether to retry it. A cancelled one is the app saying "this was never going to work and that's fine". Using an ordinary error where `JobCancel` belonged is how a dashboard fills up with noise that nobody reads any more.

> **Don't do this.** Write the retry yourself:
>
> ```go
> for attempt := range 5 {
>     if err := send(ctx); err == nil {
>         return nil
>     }
>     time.Sleep(time.Duration(attempt) * time.Second) // holds a worker slot; lost on deploy
> }
> ```
>
> A loop like this holds a worker slot for minutes, loses all its progress when the instance restarts, is invisible in `/ops/jobs/runs`, and can't be changed without a deploy.

> **Do this instead.** Return the error and let `MaxAttempts` count. The number is in the definition, so an operator can raise it during an incident; the backoff is River's; and a stuck job is a row an operator can see rather than a goroutine sleeping inside your process. [Chapter 17](17-extending-the-framework.md) leans on this hard: a whole webhook delivery system with no `for` and no `time.Sleep` anywhere in it.

The late sweep declares `MaxAttempts: 1` for a reason worth copying. It is a sweep: if this run fails, the next one five minutes later looks at the same data and does the same work. Retrying would be doing the next run early.

## 6. What an operator can change without you

**What we're doing.** Handing the dial to the people on call.

**Why.** A job's *code* is a deploy. A job's *configuration* should not be: "turn the sweep off, it's hammering the database during the migration" is a thing somebody needs to do at 02:40, and a pull request is not the right tool.

**What the framework already gives us.** `/ops/jobs/definitions/{name}`, with history, versioning and audit. Nothing to write.

**What we build ourselves.** Sensible code defaults, which is what the `Definition` literal in step 1 is.

**How.** Every field is editable through `PUT /ops/jobs/definitions/{name}` ([ops API reference](../guides/ops-api.md#job-definitions)):

```bash
curl -X PUT http://127.0.0.1:8080/ops/jobs/definitions/orders_late_sweep \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"schedule":"@every 15m","version":0,"reason":"the database is busy during the migration"}'
```

| Field | Allowed | A change applies to |
|---|---|---|
| `enabled` | true/false | Disabled: the schedule stops and "run now" is refused. Jobs enqueued by code still run |
| `schedule` | Cron in UTC, a descriptor, or empty | The next schedule, within seconds, on the leader |
| `timeout` | 1s to 24h | Attempts that start after the change |
| `max_attempts` | 1 to 100 | Jobs enqueued after the change |
| `queue` | A queue some worker runs | Jobs enqueued after the change |
| `priority` | 1 (highest) to 4 | Jobs enqueued after the change |

`version` is the one you read, so two operators editing at once get a 409 rather than one silently overwriting the other. A **reason is required** to disable a job, to change an enabled job's schedule, or to change its timeout, max attempts or queue — each of those can stop a job doing its work while it still looks healthy, and six months later somebody will want to know why the sweep runs every four hours. The reason lands in the definition's history and in the `jobs.definition.changed` audit event, which [chapter 18](18-audit-logs-and-observability.md) reads back.

`DELETE /ops/jobs/definitions/{name}` puts everything back to the code defaults.

## 7. Be honest: jobs emit no metrics

This is the one place the job system will disappoint you, and it is better to know now than to discover it during an incident.

gorbital's live observability counts **HTTP requests**: requests, 4xx, 5xx and latency, per route, per minute ([observability guide](../guides/observability.md)). Jobs are not requests, and nothing counts them. There is no queue-depth gauge, no job-duration histogram, no failure counter — not in `/ops/observability`, and not as OpenTelemetry metrics either.

What you do get:

- **Traces.** A worker runs inside a consumer span named `job <kind>`, a child of the trace that enqueued it, so a slow job is visible in a tracing backend.
- **Logs.** One line per failed attempt, with the error; a panic is logged with its stack.
- **Audit events** you write yourself, which is what chapter 17's delivery worker does.
- **`GET /ops/jobs/overview`**, which is a point-in-time summary across every instance: per queue, how many jobs are `available`, `scheduled`, `running`, `retryable` and `discarded_last_day`, plus a `failing` list of definitions whose most recent run is retrying or was discarded.

So "queue depth over time" means polling `/ops/jobs/overview` on a schedule and shipping the numbers somewhere yourself. If a growing queue is something you need to page on, that poll is the piece you have to build, and it is worth building before you need it rather than during.

## 8. Testing a worker

**What we're doing.** Checking that the sweep finds the right orders and asks for the right notifications.

**Why.** A job that runs on a clock is the easiest kind of code to leave untested and the hardest to debug when it misbehaves, because by the time anybody notices, the evidence is a log line from four hours ago.

**What the framework already gives us.** [`gorbitaltest`](../guides/testing-with-gorbitaltest.md) builds the whole app against a real PostgreSQL, including the job client, so jobs enqueued by the code under test are real rows that `App.Jobs(t, kind)` reads back.

**What we build ourselves.** The worker, by hand, and a call to `Work`. Workers are not running in a test — nothing is consuming the queue — so the way to test one is to construct it and call it:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-late-sweep -->

**What just happened.** Three different things were checked, and none of them needed the job system to be running. The worker did nothing when nothing was late. It produced exactly one fanout job when one order was late, which `App.Jobs` read out of the queue table. And `LateOrders` was called directly, which is only possible because the rule lives in a use case rather than inside `Work`.

Note the job value the test passes: `&river.Job[usecase.LateSweepArgs]{JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, MaxAttempts: 1}}`. `Attempt` and `MaxAttempts` are there because a worker may legitimately behave differently on its last attempt — chapter 17's delivery worker writes an audit event only then — so a test that wants that behaviour has to say which attempt it is pretending to be.

## Where to go next

- The whole job system, including queues, wiring, email delivery and the Go API for managing runs: [background jobs guide](../guides/background-jobs.md).
- Every job a Full app defines, with its default schedule: [jobs reference](../reference/jobs.md).
- The `/ops` endpoints in full: [ops API reference](../guides/ops-api.md#job-definitions).
- Next chapter: [runtime settings and feature flags](14-settings-and-feature-flags.md), which is where `orders.late_after` comes from and how an operator moves it.
