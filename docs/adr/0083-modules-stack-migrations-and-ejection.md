# ADR-0083: Modules, the default stack, migrations and ejection

**Status:** Accepted (2026-09-17) · **Amends:** ADR-0017, ADR-0022, ADR-0039, ADR-0050 · **Builds on:** ADR-0081, ADR-0082

## Context

ADR-0081 moves the wiring of `internal/app` into `gorbital.dev/gorbital`, and ADR-0082 defines routes and guards. This record decides what a module is, what the default middleware stack contains, where built-in modules live, how migrations from the library and the app form one history, and how a developer takes a built-in module back into their own code.

Facts it relies on, checked on 2026-09-17:

| Area | Today | Evidence |
|---|---|---|
| Middleware order | recover → trusted proxies → request ID → telemetry → observability → access log → secure headers → CORS → cross-origin protection → body limit → maintenance → authentication → dev operator (development) → per-IP limit on `/v1/auth/` → idempotency | `examples/full-single/internal/app/routes.go` |
| Library migrations | Embedded per module with local numbers (`modules/settings/migrations/00001_settings.sql`, `modules/flags/migrations/00001_flags.sql`, …) and copied into the app with app versions and identical content (`db/migrations/20260914000001_settings.sql`, `20260918000010_flags.sql`, …) | byte comparison of every golden-app migration with its module source |
| App-only migrations | Sign-in (`20260915000001_auth.sql`, `…_auth_mfa`, `…_auth_passkeys`, `…_auth_social`, `20260917000001_auth_token_revocations`, `20260918000020_auth_api_keys`, `…030_auth_github`, `…070_auth_bans`) and the example resource | same |
| Running migrations | goose from `cmd/migrate`, never at startup; River's migrations after goose's | ADR-0017, `internal/app/migrate.go` |
| Declarations | Permissions in `permissions.go` (`auth.Catalog`), settings in `settings.go`, flags in `flags.go`, jobs in `jobs.go`, error mappings in each `module_<name>.go` | `internal/app` |

## Options

### Where built-in modules live

| Option | Verdict |
|---|---|
| `gorbital.dev/modules/auth/authhttp` and similar, beside the building blocks | Rejected: a built-in module returns a `gorbital.Module`, so it must import the composition layer; modules may not depend on the layer above them (ADR-0019), and the Go modules would require each other |
| One new Go module per built-in (`gorbital.dev/authhttp`, …) | Rejected: five more modules to tag and release for packages always released together |
| **Packages inside the composition module: `gorbital.dev/gorbital/authhttp`, `opshttp`, `orgshttp`, `flagshttp`, `mailevents`** | **Chosen**: they are the composition layer's own modules; an app compiles only the ones it imports |

### Migrations

| Option | Verdict |
|---|---|
| A version table per module | Rejected: loses the order across modules (an app table referencing `auth_users` must run after it) and changes existing databases' goose history |
| Keep copying library migrations into `db/migrations` | Rejected: every built-in change becomes a file the app must receive through an upgrade |
| **One goose history. Built-in files are served from the library under their app versions; `migrate` merges them with `db/migrations`** | **Chosen**: an upgraded v0.1 database sees the same versions, already applied |

### Taking a module back

| Option | Verdict |
|---|---|
| No escape hatch: hooks only | Rejected: some apps will need changes no hook anticipates |
| Fork the library | Rejected: loses every other fix |
| **`orb eject <module>`: copies that module's source, at the library version in `go.mod`, into the app as owned code** | **Chosen** |

## Decision

### 1. Module

```go
type Module struct {
	Name        string                                   // unique; used in operation IDs, errors and logs
	Routes      func(r *Router, d Deps)
	Errors      []httpx.Mapping
	Permissions []Permission                             // name, description, roles that hold it
	Settings    func(r *settings.Registry)
	Flags       func(r *flags.Registry)
	Jobs        func(defs *jobs.Definitions, d Deps)
	Middleware  []httpx.Middleware
	Migrations  []Migration                              // built-in modules only; app modules use db/migrations
}
```

- Fields arrive with the phase that uses them: `Middleware` in Phase 2, `Jobs` and `Migrations` in Phase 3. `Declare` handles permissions, settings and flags; `Mount` handles errors and routes.
- A module is a value returned by its package's `func Module() gorbital.Module`. Settings and flags declared in `Settings`/`Flags` are captured in the constructor's closure and used in `Routes`: explicit, no lookup by name.
- `gorbital.New` calls every `Settings`, `Flags` and `Permissions` first, builds the stores, then every `Jobs` and `Routes`. Duplicate names (routes, operation IDs, permissions, setting keys, flag keys, job names, error codes) fail `New` naming both modules.
- A module with zero `Deps` must still register its routes, so `openapi` can export the document without a database (today's nil-module path).

### 2. Deps

```go
type Deps struct {
	DB       *pgxpool.Pool
	Audit    audit.Recorder
	Mailer   mail.Sender        // queued through jobs, defaults from the mail.* settings
	Jobs     *jobs.Client
	Settings *settings.Store
	Flags    *flags.Store
	Storage  storage.Store      // nil unless WithStorage
	Logger   *slog.Logger       // tagged with the module's name
}
```

A plain struct: no container, no lookup by type. A field is added only when two built-in modules need it. What sign-in exposes to other modules (the user record, `SignIn` for new sign-in methods) is decided with Phase 6, under one constraint: `gorbital.dev/gorbital` must not import `authhttp`.

### 3. App layout

```text
my-api/
├── cmd/api/main.go                    gorbital.Main(...)
├── internal/modules/
│   ├── modules.gen.go                 generated list: func All() []gorbital.Module
│   └── books/                         module.go · handlers.go · store.go (flat), or domain/usecase/repository/delivery (--layered)
├── db/migrations/                     the app's own migrations
├── .env.example · compose.yaml · Dockerfile · gorbital.yaml · gorbital.lock
```

- Modules stay under `internal/` as in v0.1: not importable by other repositories, and upgrades don't move them.
- `internal/modules/modules.gen.go` lists every directory under `internal/modules` that declares `func Module() gorbital.Module`, sorted by name; written by `orb dev` and `go generate`, committed, marked `// Code generated … DO NOT EDIT.` An app builds without `orb`.
- `orb gen module` writes the flat layout by default; `--layered` keeps ADR-0039's four layers and their architecture test.

### 4. The default stack

`gorbital.Stack` has one field per step, in this order; `gorbital.New` builds it from the configuration:

| Field | Step | Source |
|---|---|---|
| `Recover` | Panic to 500 problem | `httpx.Recover` |
| `TrustedProxies` | Client address behind proxies | `httpx.TrustedProxies` (ADR-0052) |
| `RequestID` | `X-Request-ID` from trusted callers only | `httpx.RequestIDFrom` |
| `Telemetry` | Spans and metrics | `telemetry` |
| `Observability` | Request minutes per route | `observability` |
| `AccessLog` | One structured line per request | `httpx.AccessLog` |
| `SecureHeaders` | HSTS in production and security headers | `httpx.SecureHeaders` |
| `CORS` | Allowed origins | `httpx.CORS` |
| `CrossOrigin` | Cross-site protection for cookie-authenticated writes | `httpx.CrossOrigin` |
| `BodyLimit` | `APP_MAX_BODY_BYTES` | `httpx.BodyLimit` |
| `Maintenance` | 503 outside health, docs, sign-in and `/ops` while on | `httpx.Maintenance` (moved from the app) |
| `Auth` | Resolves the actor; the dev operator in development | the configured authenticator |
| `RateLimit` | Per-IP limit on the sign-in prefix | `ratelimit.Middleware` over `ratelimitpg` |
| `Idempotency` | `Idempotency-Key` on signed-in POST and PATCH | `idempotency.Middleware` |
| *(WithMiddleware)* | The app's middleware | — |

`WithStack(func(s Stack) []httpx.Middleware)` returns the full order in Go; leaving out `Recover` or `Auth` logs a warning at start and `orb doctor` reports it (ADR-0082).

### 5. Authenticator

```go
type Authenticator interface {
	Middleware(logger *slog.Logger) httpx.Middleware   // sets the actor for authenticated requests
	Module() Module                                    // its routes, permissions, settings, jobs, migrations
}
```

`gorbital.WithAuth(a Authenticator)` accepts `authhttp.New(...)` or, later, other authenticators (external JWTs, Phase 10). Without one, only public routes succeed (ADR-0082).

### 6. Migrations

```go
type Migration struct {
	Version int64   // the app history's version, such as 20260914000001
	Name    string  // such as "settings"
	FS      fs.FS   // the module's embedded files
	File    string  // the file in FS, such as "00001_settings.sql"
}
```

- `gorbital` holds the version table for library modules whose files v0.1 apps already carry (settings, jobs definitions, audit events, release instances, rate-limit buckets, settings per organisation, flags, idempotency keys, mail suppressions, observability minutes, incidents), with the exact versions of v0.1. Built-in modules (`authhttp`, `orgshttp`) declare theirs in `Module.Migrations` with the versions they have in v0.1 apps.
- `migrate` builds one in-memory file system from the built-in files (renamed to `<version>_<name>.sql`) and `db/migrations`, then runs goose and River's migrations as today:
  - the same version in both with identical content is one migration (a v0.1 app that still has its copies);
  - the same version with different content, or two built-ins with one version, fails naming both files;
  - versions and the goose table are unchanged, so migrating an upgraded v0.1 database applies nothing.
- Migrations never run at startup (ADR-0017); `/readyz` and `orb doctor` report pending ones as today.
- New built-in migrations get versions later than every released one; released migrations never change.

### 7. Ejection

`orb eject <auth|ops|orgs|flags|mailevents>`:

1. Reads the `gorbital.dev/gorbital` version from `go.mod` and copies that version's package source from the module cache into `internal/modules/<name>/`, rewriting its import path; the result is layered owned code with its tests.
2. Changes `main.go` through plan → diff → apply (ADR-0021): `WithAuth(authhttp.New(...))` becomes the local package, and options passed to the built-in are kept where the copy supports them.
3. Copies the module's migrations into `db/migrations` under the same versions (the merge in 6 treats them as identical).
4. Records the ejection and its source version in `gorbital.lock`; `orb upgrade` then treats the module as owned and reports library changes to it as notes instead of merging them.

`--dry-run` shows the plan; ejecting twice is refused.

## Why

- A `Module` value keeps each module's declarations next to its code, and removes the four edits of v0.1.
- Built-ins inside the composition module respect the dependency direction without new release units.
- A version table in the library is the only way to stop copying migrations without touching existing databases.

## Trade-offs

- The composition module's `go.mod` requires what every built-in needs (WebAuthn, OAuth), so `go.sum` lists more; binaries still contain only imported packages, and the dependency count is budgeted in [benchmarks](../benchmarks.md).
- The version table for v0.1 library migrations is a frozen list the library carries forever.
- An ejected module stops receiving fixes, by definition; `orb upgrade` names library security fixes that touch it.

## Consequences

- ADR-0022's layout remains valid for v0.1 apps; ADR-0039's resource template becomes `orb gen module --layered`.
- ADR-0050's upgrades gain an opt-in conversion to this layout (Phase 9).
- Library modules keep their embedded `Migrations` variables for apps that compose them by hand.

## Implementation

[v0.2 roadmap](../v0.2-roadmap.md): module and deps in Phase 1, stack, authenticator and migrations in Phase 3, built-ins in Phases 4–7, generators in Phase 8, ejection in Phase 9.
