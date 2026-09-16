# ADR-0017: Application lifecycle

**Status:** Accepted (2026-09-14) · **Supersedes (with ADR-0020):** ADR-0004 · **Amended by:** ADR-0053 (readiness checks shared and cached for 1 s)

## Context

The earlier `Starter`/`Stopper` interfaces were ambiguous (is `Start` blocking?), didn't handle startup failure, had no readiness drain, and didn't fit background workers. Apps include an HTTP server, job workers, a database pool and telemetry exporters that must start and stop in a correct order.

## Options

1. `Starter`/`Stopper` interfaces with ordered lists.
2. A DI container with lifecycle hooks (Fx).
3. Constructors for setup, one `Runner` interface for long-running work, `io.Closer` for resources, and a small run helper that owns the shutdown sequence.

## Decision

Option 3.

| Concern | Design |
|---|---|
| Setup | Constructors do blocking initialisation with a context timeout (open pool, ping, load keys) and return `(*T, error)`. Order is the construction order in `internal/app`, visible and compile-checked. |
| Resources | Implement the standard `io.Closer`. |
| Long-running work | `type Runner interface { Run(ctx context.Context) error }`. `Run` blocks until `ctx` is done and returns `nil` on graceful exit. Examples: HTTP server, job workers, schedulers. |
| Startup failure | A cleanup stack: every successfully constructed resource is registered; if a later constructor fails, registered resources close in reverse order and the process exits non-zero. |
| Running | Runners run under `errgroup`; the first unexpected error cancels the others. |
| Shutdown | 1. Signal received → `/readyz` returns 503. 2. Drain wait (default 5s) for load balancers. 3. Cancel the runner context; HTTP server shuts down gracefully, workers stop taking jobs. 4. Hard deadline (default 25s). 5. Close resources in reverse order. 6. Flush telemetry last. 7. A second signal forces exit. |
| Telemetry | Constructed first, flushed last. |
| Migrations | Never run implicitly at startup. Separate `migrate` command; optional startup flag, off by default in production. |

Core package `gorbital.dev/app` provides `Runner`, the cleanup stack and the run helper (stdlib + `x/sync/errgroup` only).

## Why

- Correct behaviour on deploys: no dropped requests, no leaked goroutines, no lost telemetry.
- No container, no reflection; the lifecycle reads top to bottom in `internal/app/app.go`.

## Trade-offs

- Drain and deadline defaults must be tuned per platform (documented, configurable).
- Developers write construction order by hand (the generator writes the initial version).

## Consequences

- Every official module states whether its types are `Runner`, `io.Closer`, both or neither.
- Jobs and HTTP must respect context cancellation; tests cover graceful shutdown.

## Security review fixes (2026-09-16)

HTTP-7: `/readyz` is public and ran every check, a PostgreSQL ping holding a pooled connection, on every request, so a flood of readiness requests could starve the pool and make the load balancer's own probes fail on every instance. `health.Checker.Readiness` now shares one run of the checks among concurrent requests and reuses its result for one second; `Checker.Check` still runs them on every call, and `Add` discards a cached result. A run started by a request isn't canceled with it: each check keeps its own timeout. `/version` exposing the build version, Go version and commit is accepted as documented behaviour; the production guide says to block `/livez`, `/readyz` and `/version` at the load balancer for internet clients where possible.

| Check | Result |
|---|---|
| `health` `TestReadinessSharesChecks` (race detector) | 51 readiness requests during a slow check run it once and all answer 200 (51 runs without the fix) |
| `health` `TestReadiness` | A check added after a passing result is run on the next request; shutdown still answers 503 at once |
