# ADR-0019: Module dependency rules and core budget

**Status:** Accepted (2026-09-14) · **Supersedes (with ADR-0021):** ADR-0002 · **Amends:** ADR-0001, ADR-0007, ADR-0009 · **Amended by:** ADR-0033

## Context

The v1 design made auth a hub that imported postgres, email, jobs, audit and rate-limit modules, so any of them forced an auth release and every auth user pulled River. Core was planned to include the OpenTelemetry SDK and exporters (gRPC, protobuf), an env loader and a validation library, all inherited by every dependent. Modules risked becoming a monolith split into packages.

## Options

1. Modules freely import each other's public APIs.
2. A layered chain: core → official modules → community modules → app.
3. A star: modules depend only on core contracts; composition happens in the app.

## Decision

Option 3.

```text
generated app ──► modules/* ──► core ──► stdlib (+ OpenTelemetry API, golang.org/x)
                  modules never import other modules
```

### Rules

1. **Allowed:** app → any module; module → core; module → its own third-party libraries.
2. **Forbidden:** module → module; core → anything outside its budget.
3. **Core admission:** a contract enters core only when at least two official modules consume it.
4. **Core dependency budget:** standard library, OpenTelemetry **API** (not SDK), `golang.org/x/*`, and the OpenTelemetry API's own small dependencies (currently `github.com/cespare/xxhash/v2`). Enforced by `internal/archtest/budget_test.go`. In v0.1 core links only `go.opentelemetry.io/otel/trace` (and its internal packages) and `golang.org/x/time/rate`.
5. **Heavy integrations are modules:** OpenTelemetry SDK and exporters → `modules/telemetry`; Huma → `modules/openapi`; Postgres test helpers → `modules/postgres/pgtest`. Request validation comes from Huma schemas in delivery layers, so core has no validation library.
6. **Community modules** follow the same rules.
7. **Enforcement:** CI checks import rules per module (`go list -deps`) and fails if core's `go.mod` gains a dependency outside the budget.

### Interfaces

| Interface | Location | Shape |
|---|---|---|
| `audit.Recorder` | core `audit` | `Record(ctx, audit.Event) error`; transactional writes are a concrete method on `auditpg.Store` |
| `mail.Sender` | core `mail` | `Send(ctx, mail.Message) error`; `Message` is a struct |
| `app.Runner` | core `app` | `Run(ctx) error` (ADR-0017) |
| `health.Check` | core `health` | A struct `{Name, Timeout, Func}`, not an interface |
| Limiter (auth) | `modules/auth` | Consumer-owned; default implementation from core `ratelimit` |
| `Authorizer` | Not defined in v1 | Added when the first external policy adapter exists |

Removed: generic `errs` kinds package, `ErrorReporter` hook, any logger interface (use `*slog.Logger` and `slog.Handler`), capability discovery by type assertion.

### Cross-module needs (solved in the app)

| Need | Solution |
|---|---|
| Auth sends email asynchronously | Auth takes `mail.Sender`; app passes `jobs.AsyncSender(provider)` |
| Auth and orgs write audit events | Both take `audit.Recorder`; app passes `auditpg.Store` |
| Orgs needs the current user | Reads `actor.From(ctx)` from core |
| Ops shows audit, jobs, releases | The app's `ops` module calls each module's query API |

## Why

Independent release cycles, small dependency graphs for users, and no hidden coupling.

## Trade-offs

- More wiring in the app, written by the generator.
- Some duplication of small interfaces between consumers.

## Consequences

- The core module lives at the repository root (`module apistock.dev`), as `go-import` maps `apistock.dev` to the root.
- New official modules must pass the import-rule and dependency-budget checks.
