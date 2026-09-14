# Architecture

This app follows the apistock layered module structure. The rules below are checked by `internal/app/architecture_test.go`, so `go test ./...` fails when they are broken.

## Layout

```text
cmd/api/                 entry point: config → app → run ("api openapi" exports the spec)
cmd/migrate/             applies db/migrations, then the job queue's migrations
db/migrations/           one ordered goose history, including apistock module tables
internal/app/            composition root: builds, wires, runs and shuts down the app
  config.go              boot configuration: secrets and infrastructure from environment variables
  settings.go            runtime settings: tunables edited through /ops/settings
  jobs.go                one line per background job (//aps:anchor jobs)
  job_<name>.go          declares one job and its default configuration
  app.go                 construction order and lifecycle
  routes.go              API, health, docs and the middleware chain
  ops_auth.go            interim OPS_TOKEN protection for /ops/*
  modules.go             one line per business module (//aps:anchor modules)
  module_<name>.go       wires one module: its operations and error codes
internal/jobs/<name>/    background job arguments and worker
internal/modules/<name>/ one bounded context per directory
  module.go              wires the module's layers
  domain/                business rules and errors (standard library only)
  usecase/               application logic; ports.go holds the interfaces it needs
  repository/            storage adapters implementing ports (hand-written SQL)
  delivery/              HTTP adapter: Huma operations ↔ use cases
internal/modules/ops/    admin APIs for runtime settings and jobs
api/openapi.json         exported API contract (committed; review changes in pull requests)
compose.yaml             PostgreSQL for development and tests
```

## Request flow

```text
HTTP → middleware (recover, request ID, tracing, access log, security headers, CORS,
       cross-origin protection, body limit, ops token) → delivery → usecase → domain
     ← domain errors mapped to problem+json in internal/app/module_<name>.go
```

## Configuration

- **Environment** (`config.go`): secrets and infrastructure. Changing them needs a restart.
- **Runtime settings** (`settings.go`): non-secret tunables stored in PostgreSQL, changed with `PUT /ops/settings/{key}`, applied on every instance within moments. Modules receive them as `config.Value[T]` and call `Get` each time.
- **Job definitions** (`job_<name>.go`): each job's code defaults (enabled, schedule, timeout, retries, queue), overridable with `PUT /ops/jobs/definitions/{name}`. Changing a job's code still needs a deploy.

A value is never in more than one layer, and secrets are never runtime settings.

## Background jobs

Jobs run in the API process on PostgreSQL (River). A job carries the request ID, trace and actor that enqueued it, but never their permissions; it runs as the `jobs` system actor. Add one with `aps gen job <Name>`, or copy `internal/jobs/heartbeat` and `internal/app/job_heartbeat.go` and add a line in `jobs.go`.

## Rules

1. `domain/` imports only the standard library. Domain types have no struct tags.
2. `usecase/` never imports `delivery/`, `repository/` or HTTP packages.
3. `delivery/` calls use cases, never repositories. Only `delivery/`, `module.go` and `internal/app` import the HTTP framework (Huma).
4. Modules never import other modules. Cross-module needs go through interfaces wired in `internal/app`.
5. Only `internal/app` reads environment variables.
6. Handlers return domain errors unchanged; each `module_<name>.go` maps them to an HTTP status and a stable error code.
7. Request body types tolerate unknown fields (`additionalProperties:"true"`), so older servers accept newer clients.
8. Migrations never run at startup; run `cmd/migrate` before starting a new version.

## Adding an endpoint

1. Add the rule and its error to `domain/`.
2. Add the use case to `usecase/`.
3. Add input/output types and a `huma.Register` call in `delivery/`.
4. Map new domain errors in `internal/app/module_<name>.go`.
5. Run `go test ./...`, then `go run ./cmd/api openapi > api/openapi.json`.
