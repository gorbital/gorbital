# ADR-0083: Modules, the default stack, migrations and ejection

**Status:** Accepted (2026-09-17); amended in Phase 3 (2026-09-17): the layered module layout, the authenticator's optional methods, and [implementation notes](#phase-3-implementation-notes-2026-09-17) · **Amends:** ADR-0017, ADR-0022, ADR-0039, ADR-0050 · **Builds on:** ADR-0081, ADR-0082

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
	Middleware  []func(http.Handler) http.Handler
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
│   └── books/
│       ├── module.go                  func Module() gorbital.Module: name, errors, permissions, routes
│       ├── domain/                    book.go, errors.go: types and rules, standard library only
│       ├── usecase/                   service.go, ports.go, then create_book.go, get_book.go, … one file per operation
│       ├── repository/                store.go, then insert_book.go, select_book.go, … one file per operation
│       └── delivery/                  routes.go (the whole route table), responses.go, then create_book.go, … one file per operation
├── db/migrations/                     the app's own migrations
├── .env.example · compose.yaml · Dockerfile · gorbital.yaml · gorbital.lock
```

- Modules stay under `internal/` as in v0.1: not importable by other repositories, and upgrades don't move them.
- `internal/modules/modules.gen.go` lists every directory under `internal/modules` that declares `func Module() gorbital.Module`, sorted by name; written by `orb dev` and `go generate`, committed, marked `// Code generated … DO NOT EDIT.` An app builds without `orb`.
- **Layered by default, one file per operation in each of `usecase/`, `repository/` and `delivery/`** (maintainer decision, 2026-09-17). The route table stays in `delivery/routes.go`, so a module's routes and guards read in one place; each operation's input, output and handler sit in its own delivery file. The rules of ADR-0039 hold: `domain` imports the standard library only, `usecase` defines the ports `repository` implements, `delivery` never imports `repository`; `module.go` wires the layers. `orb gen module` writes this tree (Phase 8).
- The flat layout (`module.go`, `handlers.go`, `store.go`) is dropped: operation files of different layers would collide in one package (two `update_book.go`), and the maintainer prefers one layout for every module.

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
| `Timeout` | `APP_REQUEST_TIMEOUT` (default 30s): context deadline and 503 `request_timeout` (added with Phase 10, ADR-0085) | `httpx.Timeout` |
| `SecureHeaders` | HSTS in production and security headers | `httpx.SecureHeaders` |
| `CORS` | Allowed origins | `httpx.CORS` |
| `CrossOrigin` | Cross-site protection for cookie-authenticated writes | `httpx.CrossOrigin` |
| `BodyLimit` | `APP_MAX_BODY_BYTES` | `httpx.BodyLimit` |
| `Maintenance` | 503 outside health, docs, sign-in and `/ops` while on | `httpx.Maintenance` (moved from the app) |
| `Auth` | Resolves the actor; the dev operator in development | the configured authenticator |
| `RateLimit` | Per-IP limit on the sign-in prefix | `ratelimit.Middleware` over `ratelimitpg` |
| `Idempotency` | `Idempotency-Key` on signed-in POST and PATCH | `idempotency.Middleware` |
| *(WithMiddleware)* | The app's middleware | — |

`WithStack(func(s Stack) []func(http.Handler) http.Handler)` returns the full order in Go; leaving out `Recover` or `Auth` logs a warning at start and `orb doctor` reports it (ADR-0082).

### 5. Authenticator

```go
type Authenticator interface {
	Middleware(logger *slog.Logger) func(http.Handler) http.Handler // sets the actor for authenticated requests
}
```

`gorbital.WithAuth(a Authenticator)` accepts `authhttp.New(...)` or other authenticators (external JWTs, Phase 10). Two optional methods, found with a type assertion, let an authenticator bring more (decided in Phase 3: an authenticator that only resolves tokens, such as a JWT verifier, has no module or commands to return):

- `Module() Module` contributes its module (routes, permissions, settings, jobs, migrations), added before the app's modules;
- `Commands() []Command` contributes subcommands of `Main`, such as the role commands of Phase 5.

Without an authenticator, only public routes succeed (ADR-0082), and `New` logs how many routes are unreachable.

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

## Phase 3 implementation notes (2026-09-17)

What `gorbital.dev/gorbital` builds, and the decisions made while building it. Every item of Phase 3 is in the [roadmap](../v0.2-roadmap.md#phase-3-the-app-config-lifecycle-and-test-kit).

### Construction

`LoadConfig(config.Source) (Config, error)` → `New(ctx, cfg, opts...) (*App, error)` → `(*App).Run(ctx)`, with `Handler`, `Deps` and `Close`. `Main(opts...)` wraps them. `New` builds, in order: logging and telemetry (with the dev console's log buffer and the hourly log archive), the metrics listener, the pool (health check, pending-migration and row-level security warnings), `auditpg`, the settings and flags registries with the built-in settings and every module's declarations (`Declare`), the permission catalog with a role per role name the modules' permissions use, the settings and flags stores, the mail delivery sender and suppression list, `ratelimitpg` with the `auth_ip` limiter, idempotency keys, file storage (then the log archive's binding), request minutes, the job definitions (built-in jobs, then `Module.Jobs`), the job client and manager, the queued mailer, the release tracker, the request collector, the dev console, the API (`/version`, modules' routes, the `Idempotency-Key` parameter, docs, problem+json 404), health endpoints, local storage links, and the stack.

| Built in `New` | As in `full-single` | Not yet, and when |
|---|---|---|
| Built-in settings | `mail.*`, `auth.ip_requests_per_minute`, `audit.retention`, `ops.history_retention`, `releases.instance_retention`, `idempotency.retention`, `observability.retention`, `incidents.*`, `maintenance.*`, `logs.archive.enabled`, with the same keys, defaults and rules | Sign-in's `auth.*` settings: Phase 5, declared by `authhttp`; `example.ping_message` is the golden app's own |
| Built-in jobs | `ratelimit_cleanup`, `idempotency_cleanup`, `observability_cleanup`, `incidents_detect`, `retention` (targets: audit events, settings, flags and job definition history), same names, schedules and workers (`gorbital/internal/builtinjobs`) | `auth_cleanup`, `auth_revoke_tokens`: Phase 5; `heartbeat` is the golden app's example job |
| Dev console | `/_dev/` app, routes, config, migrations, jobs, Mailpit mail | The dev operator on `/ops/` (needs the ops permissions, Phase 4) and email previews (sign-in's emails, Phase 5) |
| Stack | Every step of `routes.go` | The dev operator step, with `/ops` in Phase 4 |

**Pending migrations** are logged as a warning by `New` and reported by `migrate --status`, the dev console and (Phase 4) `/ops/system`. The roadmap said `/readyz` reports them "as today"; it doesn't in v0.1 either, and a readiness failure for a pending migration would take every instance of a rolling deploy out of rotation, so readiness stays a database ping.

**`Module.Jobs` and the construction cycle.** The job client is built from the definitions, and the mailer modules send through queues on that client, so `Jobs` runs before either exists. In the `Deps` it receives, `Jobs` is nil and `Mailer` forwards to the queued mailer `New` builds next: a worker keeps `d` and uses it when a job runs, and a worker that enqueues gets the client from its context (`river.ClientFromContext`). A job defined twice fails `New` naming both modules (or `gorbital`, for a built-in job's name).

### Options

| Option | Why it exists |
|---|---|
| `WithModules`, `WithAuth`, `WithMiddleware`, `WithMiddlewareFunc`, `WithStack`, `WithLogger` | As decided in ADR-0081 and ADR-0082 |
| `WithStorage`, `WithMailer` | The app passes drivers gorbital doesn't import (ADR-0081 §1) |
| `WithStorageFunc`, `WithMailerFunc` | **Added.** `Main` loads the configuration, so `main.go` can't build an S3 store or a Resend sender from `STORAGE_*` or `RESEND_API_KEY` before calling it; these receive the loaded `Config`. An error from them is a configuration error (exit 2) |
| `WithMigrations` | **Added.** The app's own `db/migrations` are an embedded file system in the binary; `Migrate`, the pending-migration warning, the dev console and `gorbitaltest` need them. Reading `db/migrations` from the working directory would break in containers |
| `WithName` | **Added.** The service name in logs, traces, metrics, the OpenAPI title and the pool's application name (`ServiceName` in v0.1 apps). Default: the last element of the main module's path |

Defaults: file storage is the local driver for `STORAGE_DRIVER=local` (development's default); the S3-compatible drivers need `WithStorageFunc`, and `New` says so. Email is delivered over SMTP to `DEV_MAIL_SMTP_ADDR` (`devmail`) or `MAILPIT_SMTP_ADDR` (`mailpit`), and through the `WithMailer` sender for `provider`, which production requires; without one, `New` refuses (exit 2).

### Configuration: what moved out of `LoadConfig`

`LoadConfig` reads every variable of `full-single`'s `config.go`, `social.go`, `passkeys.go`, `infra_mail.go`, `storage.go` and `devconsole.go`, with their names, defaults, messages and production refusals, and reports all problems at once. Checks that need a provider's own package moved to that provider, so an app without sign-in or Resend doesn't compile them (go-webauthn, go-tpm and the OAuth packages add about 30 packages):

| Check in v0.1 | Now |
|---|---|
| `AUTH_ENCRYPTION_KEYS` required in production | The authenticator's (Phase 5): an app without sign-in, or with external JWTs, has no second-factor secrets to encrypt. The format is still checked when set |
| Parsing `APPLE_PRIVATE_KEY`, `WEBAUTHN_APPLE_APP_IDS`, `WEBAUTHN_ANDROID_APPS`; origins on the RP ID | The authenticator's (Phase 5); `Config.Auth` holds the raw values |
| `RESEND_API_KEY` required for `MAIL_DELIVERY=provider` | `New` requires a provider (`WithMailer`); the provider checks its own key |
| `RESEND_WEBHOOK_SECRET` format | The mail events module's (Phase 4) |

### Commands and exit codes

`Main` serves `serve` (default), `migrate [--status [--json]]`, `migrate-down` (development only), `openapi [--dir]` (development defaults, no environment, modules with zero `Deps`) and `version [--json]`. Exit codes: 0; 1 for runtime errors; 2 for usage and configuration errors (`ErrUsage`, invalid configuration, a refused production operation, a missing provider). Built-in modules contribute commands through `Commands() []Command` on the authenticator; names can't shadow the built-in ones. `migrate --status --json` keeps the shape of v0.1's `cmd/migrate --status --json`, so `orb doctor` reads both; `orb dev` runs `./cmd/api migrate` and `migrate-down` in apps without `cmd/migrate`.

### Migrations

`Migrate` merges the frozen table of `libraryMigrations` (the eleven files listed in §6, byte-for-byte equal to both golden apps' copies, checked by a test), `Module.Migrations` and the app's files. Only `.sql` files in the root of the app's file system count, named `<positive version>_<name>.sql` (fuzzed). The merged files are served from memory to `postgres.Migrate`, so the goose table and versions are v0.1's; River's migrations run after. Tested: a database migrated by `full-single`'s and `full-multi`'s v0.1 `cmd/migrate` applies nothing, with or without the app's copies of the library files.

### The middleware stack

`Stack` fields are `func(http.Handler) http.Handler`, like `Module.Middleware`, so any middleware fits without conversion. `WithStack` can't be checked by comparing functions (Go can't compare them); instead `New` wraps `Recover` and `Auth` so building the handler records whether they were used, and logs a warning when either wasn't. Maintenance is `httpx.Maintenance(MaintenanceOptions)`, with the open paths passed in (health checks, docs, `/.well-known/`, `/ops/`, `/v1/auth/`) because two of them are app routes. The Apple sign-in callbacks stay exempt from cross-origin protection in the stack, so `authhttp` works unchanged in Phase 5.

### The test kit

`gorbital.dev/gorbital/gorbitaltest` is a package of the composition module rather than its own module: it needs gorbital's options and is released with it, and a test-only package costs apps nothing at build time (`pgtest` is the precedent). `New(t, opts...)` creates a database with `pgtest.NewDatabase`, runs `gorbital.Migrate` and `gorbital.New`, and closes the app when the test ends (about 0.2 s per test on the repository's Docker PostgreSQL). Principals (`User`, `APIKey`) are put in the request's context with `auth.WithPrincipal` before the stack, as the authenticator would; the test app's own authenticator passes requests through, and one the test passes replaces it. Workers don't run in tests, so `Mail` and `Jobs` read what was queued from `river_job` (mail's JSON field names are fixed by `modules/jobs`) instead of recording deliveries.

### D17: the Minimal preset, re-evaluated with numbers

Measured with `scripts/bench-baseline.sh`, 9 starts each, back to back on the same machine ([benchmarks](../benchmarks.md#measured-gorbitalmain-apps-v02-phase-3)):

| App | Packages | Stripped binary | Startup to `/readyz` | RSS at ready |
|---|---|---|---|---|
| `examples/minimal` (core only, no database) | 456 | 17.7 MiB | 54 ms | 19.5 MiB |
| `examples/apps/shelfie` (`gorbital.Main`, one module) | 584 | 24.9 MiB | 58–74 ms | 29.2 MiB |
| `examples/full-single` (v0.1 wiring, sign-in and `/ops`) | 674 | 34.2 MiB | 145 ms | 56.5 MiB |

**Decision: D17 stands.** An app on `gorbital.New` carries 128 more packages, 7.3 MiB more binary and about 10 MiB more memory than Minimal before it has a feature, and requires PostgreSQL; making `New` database-optional would add a branch to most construction steps for apps that don't need what `New` provides. Minimal keeps composing core packages directly; `orb new --preset minimal` stays as it is in Phase 9.

### Threat model

| Threat | Mitigation |
|---|---|
| An app without an authenticator exposes routes | Deny by default still holds: every non-public route answers 401; `New` logs how many are unreachable |
| A custom stack drops recovery or authentication | Allowed, logged at start; `orb doctor` reports it (Phase 8) |
| `gorbitaltest`'s pass-through authenticator reaches production | It is unexported in a test package; `Main` has no test mode |
| A migration conflict silently changes an existing database | Conflicting content for one version fails naming both files before connecting; released versions are frozen and tested against the golden apps |
| Commands contributed by a module shadow `migrate` or `serve` | Refused at start (exit 2) |
| Production starts with development tools | `LoadConfig`'s refusals are v0.1's (dev console token, local storage, `devmail`/`mailpit`, http origins); `migrate-down` refuses production; `New` refuses `provider` delivery without a provider |

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
