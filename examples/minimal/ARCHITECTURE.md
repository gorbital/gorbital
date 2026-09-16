# Architecture

This app follows the gorbital layered module structure. The rules below are checked by `internal/app/architecture_test.go`, so `go test ./...` fails when they are broken.

## Layout

```text
cmd/api/                 entry point: config → app → run ("api openapi" exports the spec)
internal/app/            composition root: builds, wires, runs and shuts down the app
  config.go              every setting, read from environment variables
  app.go                 construction order and lifecycle
  routes.go              API, health, docs and the middleware chain
  metrics.go             METRICS_ADDR: Prometheus /metrics on its own listener, off by default
  devconsole.go          DEV_CONSOLE_TOKEN: development-only /_dev/ APIs (requests, logs, routes, configuration without secrets; modules/devconsole)
  modules.go             one line per business module (//orb:anchor modules)
  module_<name>.go       wires one module: its operations and error codes
internal/modules/<name>/ one bounded context per directory
  module.go              wires the module's layers
  domain/                business rules and errors (standard library only)
  usecase/               application logic; ports.go holds the interfaces it needs
  repository/            storage adapters implementing ports (added with a database)
  delivery/              HTTP adapter: Huma operations ↔ use cases
api/openapi.json         exported API contract (committed; review changes in pull requests)
```

## Request flow

```text
HTTP → middleware (recover, request ID, tracing, access log, security headers, CORS,
       cross-origin protection, body limit) → delivery → usecase → domain
     ← domain errors mapped to problem+json in internal/app/module_<name>.go
```

## Rules

1. `domain/` imports only the standard library. Domain types have no struct tags.
2. `usecase/` never imports `delivery/`, `repository/` or HTTP packages.
3. `delivery/` calls use cases, never repositories. Only `delivery/`, `module.go` and `internal/app` import the HTTP framework (Huma).
4. Modules never import other modules. Cross-module needs go through interfaces wired in `internal/app`.
5. Only `internal/app` reads environment variables.
6. Handlers return domain errors unchanged; each `module_<name>.go` maps them to an HTTP status and a stable error code.
7. Request body types tolerate unknown fields (`additionalProperties:"true"`), so older servers accept newer clients.

## Adding an endpoint

1. Add the rule and its error to `domain/`.
2. Add the use case to `usecase/`.
3. Add input/output types and a `huma.Register` call in `delivery/`.
4. Map new domain errors in `internal/app/module_<name>.go`.
5. Run `go test ./...`, then `go run ./cmd/api openapi --dir api`.
