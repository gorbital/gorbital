# ADR-0081: A framework you import

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0019, ADR-0022, the [architecture principles](../architecture.md#2-principles) · **Roadmap:** [v0.2](../v0.2-roadmap.md)

## Context

`v0.1.0` gives an app its features by generating them into it. Measured on 2026-09-17:

| Area | Today | Evidence |
|---|---|---|
| Wiring | `internal/app` constructs every store, the job client, the mailer, the auth service and the middleware chain: 10 027 lines in `portal-demo` | `internal/app/{app,routes,rate_limits,maintenance,permissions,settings}.go` |
| Endpoints | Sign-in (74 operations), `/ops`, client flags and the Resend webhook are generated modules owned by the app: 16 963 lines | `internal/modules/{auth,ops,flags,mailevents}` |
| Library | The behaviour already lives in the library: `httpx` middleware and problem+json, `ratelimit`/`ratelimitpg`, `auth` building blocks, `actor`, `idempotency`, `orgs`, `postgres`, `jobs`, `settings`, `flags`, `mail`, `storage`, `telemetry`, `observability` | `api/*.txt` |
| Fixes | A fix to generated code reaches an app only through `orb upgrade`'s three-way merge, and conflicts wherever the developer edited | ADR-0016, ADR-0050 |
| Adding a module | Four edits: the module, `internal/app/module_<name>.go` (error mappings), a line after `//orb:anchor modules`, `permissions.go`; each operation is a `huma.Register` with a hand-written `signedIn` wrapper | `orb gen resource` output |

Developers asked for built-in middleware, security layers and sign-in they import instead of own, and for adding routes without editing several files.

Constraints: modules never import each other and core keeps its dependency budget (ADR-0019); only PostgreSQL is required (ADR-0014); migrations never run at startup (ADR-0017); v0 allows breaking changes but the compatibility checks run (ADR-0015, ADR-0054); an app survives without `orb` (principle 8).

## Options

### Where the wiring lives

| Option | Verdict |
|---|---|
| Keep generating it, improve the merge | Rejected: every app still owns ~27 000 lines it didn't write, and every fix is a merge |
| Put it in core `gorbital.dev` | Rejected: core can't import Huma, pgx or River (ADR-0019) |
| A dependency injection container (reflection or code generation) | Rejected: hides construction order, breaks "go to definition" (principle 1), and is one more thing to learn |
| **A composition library, `gorbital.dev/gorbital`, above the modules: it orders the calls `internal/app` makes today, and nothing else** | **Chosen** |

### What happens to v0.1 apps

| Option | Verdict |
|---|---|
| v0.2 removes or changes v0.1 API, every app converts | Rejected: nothing forces a break; the new layer only adds packages |
| **Additive: no exported identifier of v0.1 is removed or changes meaning; v0.1 apps build and pass their tests against v0.2; converting to the new layout is opt-in (`orb upgrade`)** | **Chosen** |

### How an app chooses built-in features

| Option | Verdict |
|---|---|
| Everything on by default, `Without…` options to remove | Rejected: what an app contains is invisible in its code, and every app compiles every driver (S3 client, WebAuthn, OAuth providers) |
| **One explicit line per built-in in `main.go`, written by `orb new`: `gorbital.WithAuth(authhttp.New())`, `gorbital.WithModules(opshttp.Module(), …)`, `gorbital.WithStorage(s3.New(…))`** | **Chosen**: visible, and an app compiles only what it names |

### How the app is constructed

| Option | Verdict |
|---|---|
| A single `gorbital.Run(cfg)` | Rejected: hard to test, can't be embedded in another server, and has to exit instead of returning errors |
| **`LoadConfig(config.Source) (Config, error)` → `New(ctx, cfg, opts...) (*App, error)` → `(*App).Run(ctx) error`, with `(*App).Handler()` for tests and embedding, and `Main(opts...)` as the documented convenience for `main.go`** | **Chosen** |

## Decision

### 1. Layers

```text
app (cmd/api/main.go, internal/modules/, db/migrations)
  → gorbital.dev/gorbital, gorbital.dev/gorbital/guard      new: may import several modules
    → gorbital.dev/modules/*                                 unchanged: never import each other
      → gorbital.dev core                                    unchanged: stdlib, OpenTelemetry API, golang.org/x
```

- `gorbital.dev/gorbital` is its own Go module. It is the only library package allowed to import more than one module; `internal/archtest` enforces it.
- It does not import optional drivers: storage backends, mail providers and the built-in modules (its own subpackages, such as `authhttp`) reach it through values and small interfaces the app passes in. A new app's dependency graph is no larger than v0.1's (measured in [benchmarks](../benchmarks.md)).

### 2. Package responsibilities

| Package | Owns | Does not own |
|---|---|---|
| `gorbital.dev/gorbital` | Config loading, construction order, the default middleware stack, the router over Huma, `Module` and `Deps`, commands, lifecycle through core `app.Run` | Business rules, SQL, sign-in logic |
| `gorbital.dev/gorbital/guard` | Route-level allow and deny decisions, and their OpenAPI description | Sessions, rate-limit storage |
| `gorbital.dev/gorbital/authhttp`, `opshttp`, `orgshttp`, `flagshttp`, `mailevents` (new, from Phase 4) | The endpoints generated apps hold today, as `gorbital.Module` values (ADR-0083) | Other modules' routes or middleware |

### 3. Construction and lifecycle

```text
Main(opts...)
  subcommand      serve (default) · migrate · migrate-down · openapi · roles · grant-role · revoke-role · reset-mfa · rotate-auth-keys · auth-providers
  LoadConfig      every problem reported at once, each naming its variable
  New             telemetry → pool → audit → settings and flags registries (from modules) → stores → jobs → mail → auth → modules' routes → stack → handler
  Run             core app.Run: server, job workers, release heartbeat; signals, drain, shutdown order (ADR-0017)
  exit code       0 ok · 1 runtime error · 2 usage or configuration error
```

Migrations run only from `migrate` (ADR-0017). Every step `New` performs is a public constructor an app can call itself.

### 4. API tiers

| Tier | Where | Promise |
|---|---|---|
| Public | `gorbital.dev/gorbital`, `guard`, the `…http` packages, new `httpx` functions | Listed by `apicheck`; additive in v0.2; breaking changes only with an ADR |
| Experimental | `gorbital.dev/x/…` | May change in any release |
| Internal | `internal/` | None |
| Generated | `internal/modules/modules.gen.go` | Regenerated freely, never edited |
| Owned scaffold | `cmd/api/main.go`, `internal/modules/<name>/…`, ejected modules | The developer's; `orb` changes it only through plan → diff → apply (ADR-0021) |

### 5. Principles

Principle 3 becomes: **"Library for behaviour and default wiring; generation for scaffolding and the module list."** The other principles are unchanged and bind this layer: traceable without reflection (1), owned code untouched (2), only PostgreSQL (5), secure defaults visible in code (6), an upgrade path first (7), no tooling needed to build (8).

## Why

- The behaviour is already in the library; what apps own is ordering and glue, which is exactly what a composition library is for.
- Explicit lines in `main.go` keep principle 1 and small dependency graphs, at the cost of three lines a generator writes.
- Additive changes let existing apps take v0.2 with `go get` and convert when they choose.

## Trade-offs

- **A larger public surface** to keep compatible: the composition API now, the built-in endpoints as library API from Phase 4. Mitigated by `apicheck`, frozen v0.1.0 contracts (`internal/contracts/v0.1.0`) and the additive rule.
- **Less to read in an app**, more to read in the library when debugging construction. Mitigated by keeping every step a public constructor and documenting the order.
- **`New` can grow into a god constructor.** It may only order calls to existing constructors; logic belongs in modules.

## Consequences

- ADR-0019 gains the composition layer; ADR-0022's layout remains valid for v0.1 apps and ejected modules; the new layout is ADR-0083's.
- CI builds and tests apps generated by `orb v0.1.0` against every change to the library.
- Documentation is versioned (ADR-0084): v0.1 docs stay published unchanged.

## Implementation

[v0.2 roadmap](../v0.2-roadmap.md), Phases 1–3 (composition layer), 4–7 (built-in modules), 8–9 (generators and upgrades).
