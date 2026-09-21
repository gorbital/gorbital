# ADR-0083: Modules, the default stack, migrations and ejection

**Status:** Accepted (2026-09-17); amended in Phase 3 (2026-09-17): the layered module layout, the authenticator's optional methods, and [implementation notes](#phase-3-implementation-notes-2026-09-17); amended in Phase 4 (2026-09-17): how built-in modules reach what the app built, rate limiters and retention declared by modules ([implementation notes](#phase-4-implementation-notes-2026-09-17)) · **Amends:** ADR-0017, ADR-0022, ADR-0039, ADR-0050 · **Builds on:** ADR-0081, ADR-0082
**Status:** Accepted (2026-09-17); amended in Phase 3 (2026-09-17): the layered module layout, the authenticator's optional methods, and [implementation notes](#phase-3-implementation-notes-2026-09-17); amended in Phase 5 (2026-09-17): `AuthSetup`, and [notes on moving sign-in with its threat model](#phase-5-implementation-notes-moving-sign-in-2026-09-17) · **Amends:** ADR-0017, ADR-0022, ADR-0039, ADR-0050 · **Builds on:** ADR-0081, ADR-0082
**Status:** Accepted (2026-09-17); amended in Phase 3 (2026-09-17): the layered module layout, the authenticator's optional methods, and [implementation notes](#phase-3-implementation-notes-2026-09-17); Phase 8 [implementation notes](#phase-8-implementation-notes-2026-09-17) (the generators); Phase 9 [implementation notes](#phase-9-implementation-notes-new-apps-on-the-v02-layout-2026-09-17) (new apps on the v0.2 layout, the app's surface) · **Amends:** ADR-0017, ADR-0022, ADR-0039, ADR-0050 · **Builds on:** ADR-0081, ADR-0082

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

A plain struct: no container, no lookup by type. A field is added only when two built-in modules need it. What sign-in exposes to other modules (the user record, `SignIn` for new sign-in methods) is decided with Phase 6, under one constraint: `gorbital.dev/gorbital` must not import `authhttp`. Decided: no `Deps` field; a module that needs sign-in takes the `*authhttp.Authenticator` as an argument ([Phase 6 notes](#custom-sign-in-methods-how-a-module-reaches-sign-in)).

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
- `orb gen module` writes exactly this tree, with `usecase/service.go` and `ports.go`, `repository/store.go`, HTTP tests at the module's root and `internal/modules/architecture_test.go` when the app has none; Shelfie's `shelves` module is its golden output ([Phase 8 notes](#phase-8-implementation-notes-2026-09-17)).

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
| `Timeout` | `APP_REQUEST_TIMEOUT` (default 30s): context deadline and 503 `request_timeout` (added with Phase 10, ADR-0085) | `timeout.New` (`gorbital.dev/httpx/timeout`) |
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

> [!NOTE]
> **Superseded in part by [ADR-0092](0092-what-the-framework-owns.md) (2026-09-21).** The `orb eject` command described in this section was removed in v0.3.0: since v0.2.1 `orb new` writes sign-in and organisations into the app, so an on-demand copy answers a question nobody has. The copy machinery below is unchanged and still runs, as internal plumbing, for `orb new`, `orb add orgs` and `orb upgrade --layout v0.2`; `opshttp`, `flagshttp` and `mailevents` are library-only from v0.3.0 on. What follows records the design as it stood on 2026-09-17.

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

Measured with `scripts/bench-baseline.sh`, 9 starts each, back to back on the same machine ([benchmarks](../benchmarks.md#gorbitalmain-apps-phase-3)):

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

## Phase 4 implementation notes (2026-09-17)

The operations API, client flags and email events moved from the golden apps' `internal/modules/{ops,flags,mailevents}` into `gorbital.dev/gorbital/opshttp`, `flagshttp` and `mailevents` ([roadmap](../v0.2-roadmap.md#phase-4-ops-flags-and-mail-events-in-the-library), items 53–57). The golden apps stay on v0.1 wiring (Phase 9 converts the templates); their HTTP tests run against the library modules.

### Layout of a built-in module

- `opshttp` keeps the golden module's layers under `opshttp/internal/{domain,usecase,delivery}`, unexported, with the use cases and SQL-free delivery files as they were. The public surface is what an app configures: `Module(opts...)` and `MailProvider` (`SignInMethods` and `SignInMethod` were removed when Phases 4 and 5 were integrated: [below](#integration-of-phases-4-and-5-2026-09-17)). `flagshttp` exports `Module` and `PermRead`; `mailevents` exports `Module`.
- The delivery files keep v0.1's `huma.Operation` declarations. `gorbital/internal/operation.Register` registers each on the `gorbital.Router`, translating its fields to route options (`OperationID`, `Summary`, `Description`, `Tags`, `Errors`, `Status`, `guard.Public()` for an operation without security) and panicking, reported by `Mount` naming the module, for a field it would drop. The move reads as a diff of imports and `huma.Register(api,` → `operation.Register(r,`.
- Two operations need what no route option sets: the event stream's and the incident report's response media types, and schemas added to the API's registry. **`gorbital.Customize(func(api huma.API, op *huma.Operation))`** was added for them: it runs after deny by default and the guards have built the operation, and registration fails when it changes the method, path, operation ID, security requirements or operation middleware, so it can't remove protection.

### How built-in modules reach what the app built

`/ops` reports on things that exist once per app and that `New` builds after `Deps`: the job manager, the instance ID and start time, the readiness checks, the merged migrations, the configuration, and what every module declared. The mail events module needs the configuration (`RESEND_WEBHOOK_SECRET`) and must refuse a malformed secret when the app starts.

| Option | Verdict |
|---|---|
| A `Deps` field per internal (`JobsManager`, `Health`, `Releases`, `Config`, …) | Rejected: every app module would receive eight fields only `/ops` reads, against §2's rule, and `Routes` can't return a configuration error |
| A lookup on `Deps` (by type, by name, or type assertions on `Deps.Audit`) | Rejected: a service locator; what a module needs stops being visible in its code |
| `gorbital` importing and wiring `opshttp` | Rejected: the dependency points the wrong way, and every app would compile `/ops` |
| `opshttp.Module(app)` taking internals in `main.go` | Rejected: `Main` builds the app after `main.go` runs; nothing exists to pass |
| **`Module.Platform func(p *Platform) error` and a dedicated `Platform` type** | **Chosen** |

- **`Platform`** holds `Config`, `Name`, `StartedAt`, `InstanceID`, `Health`, `Jobs` (the job manager), `MailSender` (the `mail.*` settings) and `Migrations` (merged), and the methods `RateLimiters()`, `Retention()`, `Authenticate(ctx, r)` and `OnShutdown(fn)`. It is documented as being for built-in modules; app modules use `Deps`. Its fields can grow with the built-in modules (sign-in's needs are Phase 5's).
- **`Module.Platform`** is called once by `New`, after every store, the job client, release tracking and the dev console exist and before any module's `Routes`, in module order. A module keeps the pointer in its constructor's closure, as it keeps settings (§1). An error is a configuration error (exit 2); a panic an ordinary error naming the module. It isn't called when `openapi` exports the document with zero `Deps`, so `Routes` registers the same operations with a service that never runs.
- **Stores that are only a pool wrapper** (`auditpg`, `releases`, `suppressionpg`, `observability` stores) are built by the module from `Deps.DB` instead of being exposed on `Platform`; the operations API records its audit events through `Deps.Audit` like every module. Only what has one instance per app, or state, is on `Platform`.
- **Why not two fields on `Deps`** even though two built-in modules use `Platform`: `Deps` is what every module receives and reads as the app's dependencies; `Platform` is internals that follow gorbital's own needs. Keeping them apart keeps §2's rule meaningful.

### Rate limiters and retention declared by modules (item 55)

| Field | Replaces in a v0.1 app | Behaviour |
|---|---|---|
| `Module.RateLimiters []RateLimiter` (name, keys, description) | The list in `rate_limits.go` | `Platform.RateLimiters()` returns the built-in `auth_ip`, the modules' in module order, then every limiter `guard.RateLimit` created, by name, with its key kind and routes (`guard.RateLimit on POST /v1/books`). A name declared twice, or declared and used by a guard, fails `New` naming both |
| `Module.Retention func(d Deps) []Retention` (data, setting, one of `Delete`, `Job` or `EnforcedBy`, `Oldest`) | The targets of the retention job in `app.go` and the policies of `retention.go` | Called with the `Deps` `Jobs` receives, before the job definitions. gorbital's own retention comes first (audit events, settings, flags and job definition history, request minutes, idempotency keys, release instances); the retention job deletes every `Retention` with `Delete`; a `Job` must be defined (checked once the job manager exists); duplicate data names fail naming both modules. Sign-in's accounts and organisations' deleted organisations join through their modules (Phases 5 and 7) |

### Behaviour kept, and the differences

The contract is proven by tests rather than asserted: `gorbital/internal/contract` exports the OpenAPI of an app built by `gorbital.Main` with the three modules and checks it with `openapi.CheckCompatible` against both frozen v0.1.0 documents, and also field by field: every operation and every shared schema is identical except for the `x-gorbital-guards` extension the router adds. It checks that the modules map every error code of the golden apps' `module_ops.go`, `module_flags.go` and `module_mailevents.go`, record every audit action of the golden modules, and declare every `ops.*` and `flags.*` permission of v0.1.0 with the same role grants, all present in the frozen `surface.json`. The golden apps' HTTP tests for `/ops`, `/v1/flags` and the webhook (settings, jobs, audit, releases, email, suppressions, storage, system, retention, rate limits, sign-in methods, observability overview, streams and two instances, incidents and detection, flags end to end and across instances, the Resend webhook, the dev operator) run against the library modules through a test app that stands in for sign-in with bearer tokens (`gorbital/internal/opstest` until Phase 9, when each module got its own copy in `testapp_test.go` so its tests are self-contained for `orb eject`).

| Difference | Why |
|---|---|
| A request without an actor gets 401 before its input is parsed, so an invalid body from an anonymous caller is 401 instead of 422 | Deny by default (ADR-0082) |
| Streams check their session again by running the app's `Auth` step on the stream's request (`Platform.Authenticate`) instead of calling sign-in with its token | Works with any authenticator (sign-in, `modules/jwt`); the context's values aren't carried over, so an earlier actor can't survive a revoked session. In development the dev console's operator keeps its stream, where v0.1 ended it after the first event |
| `/ops/auth/providers` lists what the authenticator reports, empty without a report | The report is the authenticator's; `opshttp` doesn't import sign-in. First an option, `opshttp.SignInMethods`; since the integration, `Platform.SignInMethods` |
| `GET /ops/mail` reports Resend unless `opshttp.MailProvider(ProviderSMTP)` | The provider is passed to `WithMailer` as a `mail.Sender`, which doesn't name itself |
| `ops.auth.write` (rate-limit resets) isn't declared by `opshttp` | v0.1 declares it for sign-in's account management; sign-in's module declares it (Phase 5), and two declarations would fail `New`. Since the integration, `ops.auth.read` is sign-in's too |
| `platform_admin` and `ops_viewer` don't require a second factor by themselves | v0.1's `permissions.go` calls `RequireMFA` on the catalog, which the authenticator applies when it builds a session's actor; `Module` has no way to say it. `authhttp.Setup` requires it for both roles (Phase 5) |
| `/ops/system` reports migrations against the merged history (library, modules and `db/migrations`) | The app's own `db/migrations` alone isn't what `migrate` applies |

### `OPS_ALLOWED_IPS` and the dev operator

- `LoadConfig` parses `OPS_ALLOWED_IPS` with `ipfilter.ParsePrefixes` into `Config.OpsAllowedIPs` and checks it with `ipfilter.New` (`gorbital.dev/httpx/ipfilter`, `httpx.IPFilter` until the [package moves](0085-security-layers.md#package-moves-2026-09-17)), reporting problems with the others. `opshttp` puts every route in a group with `gorbital.Use(ipfilter.New(allowed, nil))`: the first middleware of each operation, before the sign-in check, guards and input parsing, on the address `TrustedProxies` resolved. A stack step was considered and rejected: a custom `WithStack` could drop it silently, and it matters only when the module is there. The authenticator still runs first for refused addresses.
- The dev operator (ADR-0066) is part of the `Auth` step, as §4 lists it: the authenticator, then `devconsole.Operator("/ops/", …)` with the permissions the catalog grants `platform_admin`. Without the dev console (production refuses its token) it adds nothing.

### Mail events keep their verifier

`guard.Webhook` (ADR-0085) was not used: an app without `RESEND_WEBHOOK_SECRET` answers 404 `webhook_not_found`, which a guard verifying first would turn into 401; Huma's header validation (422 for oversized signature headers) would move after verification; and refused deliveries are logged by the use case. The module wraps `resend.VerifyWebhook`, which uses `gorbital.dev/webhook` already, exactly as a v0.1 app's `infra_mail.go`.

### Threat model

| Threat | Mitigation |
|---|---|
| A client outside `OPS_ALLOWED_IPS` claims an allowed address in `X-Forwarded-For` | The filter reads the address after `TrustedProxies`, which honours the header only from `APP_TRUSTED_PROXIES`; tested with a trusted proxy, the proxy itself and an untrusted client |
| An app module uses `Platform` to reach more than it should | `Platform` holds nothing an app's own `main.go` couldn't build or read (the job manager, configuration, health); it grants no permission. Documented as built-in only |
| `Customize` removes a route's authentication or guards | Changes to security, middleware, method, path or operation ID fail registration |
| A revoked session keeps streaming | `Authenticate` runs on a context without the request's values, so the actor comes only from the credentials; streams end within one interval, tested by signing out |
| The dev console's token operates `/ops` outside development | The console exists only with `APP_ENV=development`, `LoadConfig` refuses the token in production, and the operator checks loopback and a localhost `Host` |
## Phase 5 implementation notes: moving sign-in (2026-09-17)

The generated auth module of the golden apps moved into `gorbital.dev/gorbital/authhttp` with no behaviour change: the same 74 operations (66 mounted, 8 for organisations' service accounts kept unmounted for Phase 7), use cases, SQL (one file per operation), migrations, error codes, audit actions, permissions, roles, runtime settings, jobs, rate limiters, cookies, emails and commands. Customisation is Phase 6. Items are in the [roadmap](../v0.2-roadmap.md#phase-5-sign-in-extracted-unchanged).

### Layout

| Path | Holds |
|---|---|
| `authhttp/authhttp.go`, `module.go`, `config.go`, `limits.go`, `settings.go`, `commands.go`, `providers.go` | The public type and what a v0.1 app's `internal/app` did for sign-in: `module_auth.go` (error mappings), `permissions.go` (sign-in's permissions and roles), `settings.go` (`auth.*`), `rate_limits.go` (sign-in's limiters), `social.go`, `passkeys.go`, `keys.go` (configuration), `admin.go`, `admin_mfa.go`, `providers.go` (commands), `jobs.go` (the two jobs) |
| `authhttp/internal/{domain,usecase,repository,delivery}` | The golden app's `internal/modules/auth` layers, byte for byte except import paths, doc comments naming where declarations live, and route registration (below) |
| `authhttp/internal/delivery/jobs/{authcleanup,authrevoke}` | The golden app's job packages, unchanged (under `internal/jobs` until Phase 9 put every package under a layer) |
| `authhttp/internal/repository/migrations` | The eight migrations, numbered `00001`–`00008`, declared in `Module.Migrations` under `20260915000001`, `…04`, `…05`, `…06`, `20260917000001`, `20260918000020`, `…030` and `…070` |

The first commit of the phase copies the layers unchanged, so `git diff` of the later commits shows every change made to them.

### Public surface

`authhttp.New() *Authenticator` and the methods of `*Authenticator`: `Middleware(*slog.Logger)`, `Module() gorbital.Module`, `Commands() []gorbital.Command`, `CheckConfig(gorbital.Config) error` and `Setup(context.Context, gorbital.AuthSetup) error`. Nothing else is exported; the layers are internal. Phase 6 adds `New(opts ...Option)` compatibly with calls to `New()`, and hooks and `Deps.Auth` without changing these methods.

In `gorbital`: the type `AuthSetup` (`Name`, `Config`, `Deps`, `Permissions`, `DevConsole`, `Handle`, `MailPreviews`) and two more optional authenticator methods found by type assertion, like `Module` and `Commands`.

### How the app hands sign-in its configuration and dependencies

An explicit method, called by the app, once:

| Caller | Order | What it passes |
|---|---|---|
| `gorbital.New` | `CheckConfig(cfg)` after the modules are validated and before `DATABASE_URL` is checked or anything connects; `Setup` after the stores, the shared rate limits and `Deps` exist and every module's permissions and roles are declared, before `Module.Jobs`, the routes and the stack | `AuthSetup` with the app's `Deps`, the unfrozen permission catalog, whether the dev console is on, and `Handle` and `MailPreviews` |
| `gorbital.Main`, before a command the authenticator contributes | `CheckConfig`, then `Setup` with zero `Deps` and a catalog built from the modules' declarations without a database | The command opens its own pool, as v0.1's `openCommandDeps` did |

Rejected: a service locator in `Deps`; passing `Config` to `Middleware` (a breaking change to `Authenticator`, and `Routes` and `Jobs` need the use cases too); building sign-in lazily on the first request (configuration errors would surface on a request instead of at start). `CheckConfig` errors are configuration errors, exit code 2, reported after `LoadConfig`'s: a deployment with problems of both kinds sees `LoadConfig`'s first, where v0.1 listed them in one message.

`Setup` refuses a second call with a database (an `Authenticator` serves one app) and a missing dependency; `Middleware` panics before `Setup`, which `gorbital.New` can't do.

### Configuration

The checks Phase 3 moved out of `LoadConfig` run in `CheckConfig` with v0.1's messages: `AUTH_ENCRYPTION_KEYS` required in production; `WEBAUTHN_APPLE_APP_IDS` and `WEBAUTHN_ANDROID_APPS` parsed; `WEBAUTHN_ORIGINS` on `WEBAUTHN_RP_ID` (`passkey.New`); the Apple private key parsed once every Apple variable is present. Providers, the passkey relying party and the keyring are built from `Config.Auth` as the golden app's `social.go`, `passkeys.go` and `keys.go` build them; the app's name (`WithName`) is the passkey display name, the authenticator app issuer and the email brand, as `ServiceName` was.

### Routes

The delivery files register v0.1's `huma.Operation` literals through one helper, `route`, which passes the operation ID, method, path, tags, summary, description, success status and error statuses to `gorbital.Get`/`Post`/… and panics on any other field set (none is), so nothing is dropped silently. An operation without `Security` gets `guard.Public()`.

**Order of responses on signed-in routes.** gorbital's routes check for an actor before parsing the input (ADR-0082); v0.1's sign-in routes parse first and their use cases answer 401. An unauthenticated request with an invalid body got 422 `validation_failed` in v0.1 and would get 401 through `requireActor`. To keep v0.1's behaviour, `delivery.route` sets `route.Config.ActorCheckedByHandler`, an internal field of `gorbital/internal/route` that keeps the security requirement and the 401 in the document but leaves the check to the use case. No public option sets it. Every signed-in operation called without credentials answers 401 `unauthenticated`, or 400 or 422 for a missing or invalid body, never anything else (`TestSignedInRoutesRefuseAnonymous`), and `x-gorbital-guards` still says `authenticated`. Checking before parsing for sign-in too is a deliberate follow-up with its own changelog entry. *Since Phase 9* the field is the public option `gorbital.AuthenticateAfterInput()`, and the router checks the actor after validation instead of leaving it to the use case, so an ejected sign-in module keeps this order ([Phase 9 notes](#gorbital-internals-the-modules-imported)).

### What moved into gorbital, and what waits for Phase 4

| v0.1 `internal/app` | Now |
|---|---|
| The dev operator after authentication on `/ops/` (`devconsole.go`, `routes.go`) | `gorbital.New`'s `Auth` step: the authenticator's middleware, then the operator with `platform_admin`'s permissions, when the dev console is on |
| Email previews (`mail_previews.go`) | The dev console previews what `AuthSetup.MailPreviews` adds, and a test message branded with the app's name and `APP_PUBLIC_URL` |
| `.well-known` files for passkeys in apps (`passkeys.go`, `routes.go`) | `AuthSetup.Handle` mounts them on the mux behind the stack; `/_dev/routes` lists them |
| Role descriptions of `user`, `platform_admin` and `ops_viewer` (`permissions.go`) | `gorbital` declares those roles with v0.1's descriptions; `authhttp` declares `user` when no module grants it anything and requires a second factor for `platform_admin` and `ops_viewer` |
| `ops.auth.read` declared by the ops module | Declared by `authhttp`, whose `/ops/auth/users` reads check it. `opshttp` uses it for `/ops/auth/providers` and `/ops/auth/rate-limits` and doesn't declare it (integration) |
| `/ops/auth/providers`, `/ops/auth/rate-limits`, `/ops/retention` rows for accounts, the ops module's reauthentication | Done at the [integration](#integration-of-phases-4-and-5-2026-09-17): `Authenticator.SignInMethods`, `Module.RateLimiters`, `Module.Retention`; streams check the session with `Platform.Authenticate`, which runs sign-in's middleware |
| The sign-in methods table printed at start in development | Logged at start, one info line per method, in every environment: `New` also builds apps in tests. `auth-providers` prints the table |
| `cmd/seed` | Not in `gorbital.Main`; the golden apps keep it |

### Threat model: moving sign-in

| Threat | v0.1 | After the move | Proven by |
|---|---|---|---|
| **Session fixation** | A session token is generated at every sign-in (password, second factor, passkey, Google, Apple, GitHub) and never taken from the client; verifying an address or resetting a password ends every session | Same code | `TestAuthenticationEndToEnd`, `TestTwoFactorEndToEnd`, `TestPasskeysEndToEnd`, `TestSocialSignInEndToEnd` |
| **Cookie flags** | `__Host-session`: `Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, no `Domain`, expiring with the session; cleared with the same attributes. `__Host-oauth`: `SameSite=None` for Apple's form post, single use, bound to the state | Same code; cookie names are constants of the moved packages | `TestCookies`, `TestSocialSignInEndToEnd` |
| **CSRF with `CrossOrigin`** | Cross-site writes with cookies refused, except `POST /v1/auth/apple/callback` and `/v1/auth/apple/notifications` (single-use state with the `__Host-oauth` cookie; Apple's signature) | `gorbital`'s stack has the same step and the same two exceptions (Phase 3) | `TestSocialSignInEndToEnd` (cross-site Apple posts pass, a cross-site GitHub link start is 403), gorbital's stack tests |
| **Timing and account enumeration** | Unknown addresses do a dummy password hash; registration, resend and reset requests wait `auth.DefaultMinResponseTime`; the same response whether an account exists | Same code | The use-case tests, moved with the code |
| **Guessing through rate-limit keys** | `auth_ip` per client address (IPv6 /64) on changing `/v1/auth/` requests and redirects; `auth_login` per address and network, `auth_login_address` per address; `auth_mfa`, `auth_reauth` per user; `auth_code` per purpose and address; `auth_notice`; `auth_api_key` per network for failed keys; shared through PostgreSQL | Same names, keys and settings; `auth_ip` is gorbital's `RateLimit` step with v0.1's key function | `TestSignInLimitSharedAcrossInstances`, `TestPerIPLimitBehindTrustedProxy`, `TestPerIPLimitGroupsIPv6`, `TestChecksBehindASessionAreLimited`, `TestAPIKeysEndToEnd`, `TestSurfaceKeepsV010Names` |
| **API key hashing and scopes** | `gbk_<lookup>_<secret>`, only a SHA-256 hash stored, shown once with `Cache-Control: no-store`; scopes limit the owner's permissions; never the permissions of roles requiring a second factor; keys can't manage accounts, sessions or keys | Same code (`modules/auth` unchanged) | `TestAPIKeysEndToEnd` (no secret in audit events or jobs), `TestAPIKeyScopesCoverOwnData`, `TestServiceAccountsThroughOps` |
| **Second-factor step-up** | `platform_admin` and `ops_viewer` grant their permissions only to sessions verified with a second factor (`RequireMFA`), so never to API keys; recent reauthentication for sensitive changes | `authhttp.Setup` requires it for both roles whenever a module grants them anything, as `permissions.go` did. A module on `gorbital.Main` granting other roles can't require it until Phase 6 | `TestTwoFactorEndToEnd`, `TestAuthenticationEndToEnd` (403 `mfa_required`), `TestCommands` |
| **Impersonation outside development** | `POST /ops/auth/users/{id}/impersonate` works only with the dev console, which production refuses | `Setup` turns it on from `AuthSetup.DevConsole` (`APP_ENV=development` and `DEV_CONSOLE_TOKEN`); `LoadConfig` still refuses the token in production | `TestOpsImpersonationOffWithoutTheConsole`, `TestOpsUsers` |
| **The dev operator** | The console token acts as a system actor with `platform_admin`'s permissions on `/ops/` only, from a loopback peer with a localhost `Host`, never in a cookie | Same middleware (`devconsole.Operator`), after the authenticator in the `Auth` step, only with an authenticator and the console | `TestDevOperator`, `TestAuthSetupFromNew` |
| **Key rotation** | `rotate-auth-keys` re-encrypts second-factor secrets with the first key of `AUTH_ENCRYPTION_KEYS`; production requires keys | Same use case; the production requirement is in `CheckConfig` (exit 2) | `TestCommands`, `ExampleAuthenticator_CheckConfig` |
| **Provider token revocation** | `auth_revoke_tokens` every minute revokes Apple refresh tokens of unlinked identities and deleted accounts, with backoff | Same job, same name and schedule, defined by `Module.Jobs` | The job and use-case tests, `TestSurfaceKeepsV010Names` |
| **A route loses its protection in the move** | Signed-in operations declare `security`; their use cases refuse without an actor | The same use cases; `TestOpenAPIMatchesV010` compares each of the 66 operations and every schema they reference with the frozen v0.1.0 document, and checks `x-gorbital-guards` is `authenticated` exactly where `security` is set | `TestOpenAPIMatchesV010`, `TestSignedInRoutesRefuseAnonymous` |
| **Schema drift in a released database** | — | Migrations are byte for byte the golden apps', under the same versions; a database migrated by either golden app's `cmd/migrate` applies nothing | `TestModuleMigrationsMatchV01Apps`, `TestMigrateOnV01DatabaseIsNoOp` |
| **Misconfiguration passes silently** | `LoadConfig` refused it | `CheckConfig` runs before `New` connects and before commands | `TestAuthSetupFromMain`, `TestWebAuthnConfiguration`, `TestSocialConfiguration` |

**What changed compared with v0.1**, all outside the HTTP contract: sign-in's configuration errors are reported after `LoadConfig`'s instead of in the same list; wrong command arguments exit with 2 instead of 1 (`gorbital.Main`'s convention); the sign-in methods are logged at start instead of printed; `roles` lists the roles in the catalog's order (declared by name, `user` last when `authhttp` adds it) and the permissions of the modules the app mounts (no `/ops` permissions until Phase 4); the OpenAPI document gains `x-gorbital-guards` on sign-in's operations. **No HTTP response, cookie, audit event, stored row or name differs**: the golden app's HTTP tests pass against the library with only the operations module's endpoints replaced, and the contract tests compare the rest with the frozen fixtures.

## Integration of Phases 4 and 5 (2026-09-17)

Phases 4 (ops, flags and mail events) and 5 (sign-in) were built in parallel on `framework/phase-4` and `framework/phase-5`; each stood in for the other in its tests. Rebasing Phase 5 on Phase 4 left the seams unfinished: an app with `authhttp` and `opshttp` failed `New` (both declared `ops.auth.read`), and would have listed no sign-in methods, only three rate limiters and no accounts retention. The integration closes them so that an app with `gorbital.WithAuth(authhttp.New())` and `opshttp.Module()` reports what a v0.1 app reported, with nothing more in `main.go`.

### 1. Sign-in methods: an optional authenticator method

| Option | Verdict |
|---|---|
| Keep `opshttp.SignInMethods(func() []opshttp.SignInMethod)` and pass `auth.SignInMethods` in `main.go` | Rejected: every app with both modules writes the same line, and the report needs the configuration, which `main.go` doesn't have before `Main` loads it |
| `opshttp` imports `authhttp` | Rejected: `opshttp` would compile sign-in into every app with `/ops`, and couldn't report another authenticator's methods |
| A field or lookup on `Deps` | Rejected for the reasons of Phase 4 (§ How built-in modules reach what the app built) |
| **`gorbital.SignInMethod`, an optional authenticator method `SignInMethods(cfg Config) []SignInMethod`, found by type assertion like `Module`, `Commands`, `CheckConfig` and `Setup`, and `Platform.SignInMethods()` calling it with the app's configuration** | **Chosen** |

- The type moved from `opshttp` to `gorbital`, with the same fields, so the operations API converts it to its domain type directly. `Platform.SignInMethods` never returns nil: an app without an authenticator, or one without the method, lists `{"methods": []}` as before.
- `opshttp.SignInMethods` and `opshttp.SignInMethod` were removed rather than kept as an override: an authenticator of an app's own implements the method; a second way to say the same thing had no user. They were never released, so `apicheck` records no removal (Phase 4's listing hadn't been recorded on the rebased branch; it is now).
- `authhttp.Authenticator.SignInMethods` returns the table `auth-providers` prints (`writeSignInMethods` uses the same function), so the command, the start-up log lines and `/ops/auth/providers` can't disagree.

### 2. Rate limiters and the ops permissions

- `authhttp`'s `Module.RateLimiters` lists `auth_login`, `auth_login_address`, `auth_mfa`, `auth_reauth`, `auth_code`, `auth_notice` and `auth_api_key` with v0.1's keys and descriptions, from the table that also names the limiters `newRateLimits` creates (`TestSurfaceKeepsV010Names` checks they match). Resets need no new code: `opshttp` resets through `ratelimitpg.Store.Reset(name, key)` on `Deps.RateLimits`, the store sign-in's limiters are created on, exactly as v0.1's `rateLimits.Reset`.
- **Order.** v0.1 listed `ops_test_email` between `auth_notice` and `auth_api_key`; `Platform.RateLimiters` lists `auth_ip`, then each module's in module order, and the authenticator's module comes first, so `ops_test_email` is last. The list's order isn't part of the contract (clients look limiters up by name); matching it would need a sort key on `RateLimiter`. Documented in the ops API guide.
- **`ops.auth.read` and `ops.auth.write` are declared once, by `authhttp`**, with v0.1's roles (`platform_admin` both, `ops_viewer` read). v0.1 declared `ops.auth.read` in the ops module's list and `ops.auth.write` with sign-in's; both modules check both here (`/ops/auth/users` and `/ops/auth/providers`, `/ops/auth/rate-limits`), so one of them had to own them.

| Option | Verdict |
|---|---|
| Allow a permission declared by several modules when the declarations are identical | Rejected: weakens the duplicate check of item 25, and the two modules' descriptions would have to be kept equal by hand |
| `gorbital` declares them for every app | Rejected: apps with neither module would list permissions nothing checks |
| `opshttp` owns `ops.auth.read`, `authhttp` owns `ops.auth.write` (v0.1's split) | Rejected: an app with `authhttp` and no `opshttp` would let `platform_admin` change accounts but not list them |
| **`authhttp` owns both** | **Chosen**: the permissions are about sign-in (accounts, methods, sign-in's limits). In an app with `opshttp` and no `authhttp`, no role holds them: `/ops/auth/providers` (empty there anyway) and `/ops/auth/rate-limits` need an authenticator that grants them to its actors, and the dev console's operator, which holds `platform_admin`'s catalog permissions, gets 403 on those two paths. Recorded in the ops API guide and the admin tool recipe |

- The second factor for `platform_admin` and `ops_viewer` (v0.1's `RequireMFA`) is applied by `authhttp.Setup` whenever a module declares those roles, which `opshttp` does; the integration test signs in with a password only and gets 403 `mfa_required` from both modules' operations.

### 3. Retention

`authhttp`'s `Module.Retention` returns `deleted_accounts` (`auth.deleted_account_retention`) and `unverified_accounts` (`auth.unverified_account_ttl`), both with `Job: "auth_cleanup"` and no `Oldest`, as v0.1's `app.go` listed them. Because the authenticator's module comes first among modules and gorbital's rows come first of all, `/ops/retention` lists the nine rows in v0.1's order.

### 4. The rest, and how it is proven

`gorbital/internal/integration` builds the app with `gorbitaltest.NewWithEnv` (added, with `App.Config`, so a test can set `AUTH_ENCRYPTION_KEYS` and run `grant-role` against its database) and real sign-in: register, the code from the queued email, `grant-role … platform_admin`, 403 `mfa_required` with a password-only session, an authenticator app, a second sign-in with a recovery code. It then checks, against the golden app's source rather than copied expectations:

| Seam | Checked |
|---|---|
| `/ops/auth/providers` | The authenticator's report field by field, v0.1's keys in order, no `AUTH_ENCRYPTION_KEYS` |
| `/ops/auth/rate-limits` | Every limiter of v0.1's `rate_limits.go` with its keys and description; wrong passwords spend `auth_login_address`, a reset answers `reset: true` then `false`, an unknown limiter 404, both resets audited with the operator's ID |
| `/ops/retention` | v0.1's nine rows in order; the accounts rows' settings, job, default and next run |
| `/ops/system` | Database `ok`, sign-in's migrations in the merged history and applied |
| `/ops/auth/users`, `/ops/service-accounts` | The operator listed with `platform_admin`; a user without a role refused; a service account created and listed |
| Roles | `roles` lists `platform_admin` and `ops_viewer` with every ops permission v0.1's `permissions.go` gives them |
| Reauthentication of streams | `/ops/observability/stream` ends with `{"reason":"unauthorized"}` after `POST /v1/auth/logout`, through `Platform.Authenticate` and sign-in's middleware |
| OpenAPI | The document of `gorbital.Main` with the four modules is compatible with both frozen v0.1.0 documents under `/v1/auth/`, `/ops/`, `/v1/flags` and `/v1/webhooks/resend`, has every operation with its operation ID, and describes each of full-single's 127 as v0.1.0 did but for `x-gorbital-guards` |
| Names | Every error code, audit action, platform permission, role, setting and job of full-single's v0.1.0 `surface.json` exists in the app, but those of its example modules (`ping`, `projects`, `example.ping_message`, `heartbeat`) |

Nothing else differed. Found on the way and fixed in the same branch: the rebase had dropped Phase 4's lines from `api/gorbital.txt`, and `docs/guides/main-go.md` carried both phases' status notes.

### 5. v0.1 apps and new error codes

The v0.1.0 scaffold compatibility check failed on the rebased branch: apps generated by `orb` v0.1.0 scan the source of every `gorbital.dev` package they link for problem codes, and Phase 10 had added `request_timeout` and `ip_not_allowed` to `httpx`. `httpx.Timeout`, `httpx.IPFilter`, `httpx.ParsePrefixes` and `httpx.ErrDenyAll` moved to `gorbital.dev/httpx/timeout` and `gorbital.dev/httpx/ipfilter` ([ADR-0085](0085-security-layers.md#package-moves-2026-09-17)), the commit recording the codes in the golden apps was reverted, and the rule is in [stability](../guides/stability.md#adding-error-codes): new codes and audit actions go in packages v0.1 apps don't link.
## Phase 8 implementation notes (2026-09-17)

The generators and commands for this layout: [roadmap items 76–82](../v0.2-roadmap.md#phase-8-generators-and-cli). Guide: [Generating code](../guides/generating-code.md); commands: [CLI guide](../guides/cli.md#orb-gen-module).

### `orb gen module`

| Decision | Why |
|---|---|
| The templates (`cli/internal/recipes/module`) are ADR-0039's resource template, rearranged into §3's tree: the same fields, names, validation, keyset pagination, versioned updates, audit and owner isolation; permissions checked by `guard.Permission` in `routes.go` instead of in the use cases, as in Shelfie's books module | One behaviour for both generators, one layout; the guard checks permissions before the body is read |
| **Golden output is an example app**: `examples/shelfie/internal/modules/shelves` (at `examples/apps/shelfie` until [ADR-0093](0093-the-examples-repository.md) moved the showcase applications out and kept this one, because it is a fixture) and its migration are the unedited output, compared byte for byte by `TestModuleMatchesShelfie` (`-update` rewrites them); `TestGeneratedCodePasses` generates modules of every field shape, middleware and a guard into a copy of Shelfie, then runs gofmt, vet, golangci-lint and every test on PostgreSQL | Generated code is a product: it compiles, lints and passes its own tests in a real app, and chapter 9 includes it |
| Tests are HTTP tests through `gorbitaltest` plus domain tests; no repository or use-case tests on their own | The whole stack (guards, error mappings, SQL) is what the module promises; one database per test costs about 0.2 s |
| Field types are `orb gen resource`'s, plus **optional strings** (`name:string?`, 0–100 characters, sortable, never unique or the title); `orb gen resource` refuses `?` | A frequent need; text and enums are already optional |
| **`--org` is refused until Phase 7** (`guard.OrgMember`), with exit 2 and a message; `orb gen resource --scope org` in an app on `gorbital.Main` too. *Superseded by Phase 7: `--org` generates organisation modules ([below](#orb-gen-module---org))* | The owner-scoped variant is right today; org scoping without its guard would duplicate v0.1's use-case checks the library is replacing |
| `owner_id` has no foreign key | An app on `gorbital.Main` may not have `auth_users` (sign-in moves into the library in Phase 5) |
| Migrations get a `-- +goose Down` dropping the table | `orb dev`'s migrate-down and redo work on a fresh module; v0.1's templates are unchanged |
| Names are also checked against the identifiers the generated code declares or imports (`page`, `item`, `domain`, …) | Every accepted name compiles |
| The plan creates files only, plus `modules.gen.go` rewritten with the new module (the `orb gen modules` renderer) and the architecture test when missing; a module directory or file that exists is refused | Regeneration never touches user code (ADR-0021) |
| **Alias semantics**: in an app on `gorbital.Main`, `orb gen resource` runs `orb gen module` with the same arguments and says so on stderr; in a v0.1 app `orb gen resource` is unchanged and `orb gen module` points at it | Scripts and habits keep working; v0.1 apps keep their generator |

### `orb gen middleware`

Three kinds, each with a table-driven test: module middleware in `delivery/` (`func(http.Handler) http.Handler` whose rule returns an `*httpx.Problem`), a guard in `delivery/` (`guard.New` with status 403 and `Err<Name>Refused`, tested by mounting a test module), and global middleware in `internal/middleware`. It **never edits** `routes.go`, `module.go` or `main.go`: the plan's next steps give the line (`gorbital.Use`, the error mapping, `gorbital.WithMiddleware`). Where to place middleware is a judgement about order that a generator can't make, and an anchor in `main.go` would be the kind of shared-file edit ADR-0021 minimises. The rule lets every request through until written, so wiring it in changes nothing.

### `orb routes`

The OpenAPI document (built with `go run ./cmd/api openapi --dir`, read from `--openapi`, or in the Dev Portal fetched from the running app) is the truth about what the app serves; a `go/parser` scan adds the module, handler, registration and handler positions, and middleware (`Module.Middleware`, group and route `gorbital.Use`, as source text), matched by method and path with group prefixes resolved within a function, or by a literal operation ID. v0.1 apps are matched through `huma.Register` calls with a `huma.Operation` literal; their document has no `x-gorbital-guards`, so `guards_known` is false. Rejected: adding middleware names to the OpenAPI document (functions have no stable names at run time) and running the app to list routes (needs a database). The JSON (`routes.List`) is public API under `schemaVersion` 1.

### `orb doctor`

In apps on `gorbital.Main`: `modules.gen.go` missing or stale (fail; a build outside `orb dev` would not serve the module) and directories without `func Module()` (warn); a `WithStack` in `cmd/api` whose function literal or package function never names `Recover` or `Auth` and doesn't use `Default()` (warn), and any other stack pointed at the startup warning; `APP_REQUEST_TIMEOUT` as `LoadConfig` reads it (fail when the app would refuse to start, warn for 0 or under a second); pending migrations through `./cmd/api migrate --status --json` (the merged history). The v0.1 anchor checks don't run there.

### Dev Portal

`GET /_portal/api/routes` and the `module` and `middleware` generators call the same functions as the CLI (ADR-0077 amended). The Routes screen joins the list with the dev console's by method and path, and lists it alone while the app isn't running.

### Not built

An sqlc-based repository variant (`--sqlc`): possible later, generating queries from the same migration, but it adds a tool to every app's workflow and a second template to keep golden. Relations between generated modules, soft delete and search stay out, as in ADR-0039.

## Phase 6 implementation notes: sign-in options, hooks and custom methods (2026-09-17)

Phase 5 moved sign-in into `authhttp` unchanged; Phase 6 turns the reasons v0.1 apps edited their generated auth module into options and hooks, and lets a module add a sign-in method. Items 64–70 are in the [roadmap](../v0.2-roadmap.md#phase-6-sign-in-options-hooks-and-custom-methods). Without options, `authhttp.New()` is Phase 5's sign-in: the contract tests against the frozen v0.1.0 document pass unchanged, and a v0.1 app, which doesn't link `authhttp`, sees nothing.

### Public surface

| Added | For |
|---|---|
| `New(opts ...Option)`, `Option` | Compatible with every call to `New()`; `apicheck` records the signature change of an unreleased function |
| `MinPasswordLength`, `PasswordPolicy`, `RequireMFA`, `APIKeyMaxTTL`, `WithoutRegistration`, `Brand`, `RouteMiddleware` | Item 64 |
| `BeforeLogin`, `AfterLogin`, `OnRegister`, `Refuse`, `Refusal`, `LoginAttempt`, `LoginEvent`, `NewAccount`, `User` | Item 65 |
| `RegisterFields[T]` | Item 66 |
| `Authenticator.SignIn`, `SignInRequest`, `SignedIn`, `Authenticator.User`, `ErrUserNotFound` | Item 67 |

Invalid options (a minimum below 12, a nil hook, registration fields named `email`, `RegisterFields` with `WithoutRegistration`) are collected by `New` and returned by `CheckConfig`, which `gorbital.New` and `Main` call before anything connects (exit 2), and by `Setup`. `RequireMFA`'s roles can only be checked against the catalog, so `Setup` reports those. No option panics; `Refuse` does (below).

### Options: kept and dropped

Each option needed a reason a v0.1 app gave for editing its module. Deployment values stay in environment variables.

| Option | Verdict |
|---|---|
| `Password(MinLength, PasswordPolicy)` | **Kept as `MinPasswordLength(n)` and `PasswordPolicy(check)`**: apps set `PasswordChecker` in v0.1. The minimum only rises (12–128): a lower one would weaken the default silently. Policies run through `auth.ValidatePassword`, before hashing or any lookup, for every request, so they can't reveal whether an address has an account |
| `MFA(TOTP, RecoveryCodes)` | **Dropped; `RequireMFA(roles...)` instead.** Turning TOTP off in code while `AUTH_ENCRYPTION_KEYS` is set would lock out every enrolled account (`mfa_unavailable`); turning recovery codes off removes the only self-service recovery. v0.1 never allowed either. What apps needed, and Phase 5 recorded as a gap, is requiring a second factor for their own roles |
| `Passkeys`, `Google`, `Apple`, `GitHub` | **Dropped.** Their values differ per environment (client IDs, RP ID, origins) and include secrets, which stay in `_FILE` variables; a code override would make `/ops/auth/providers` and `auth-providers`, which report `Config`, disagree with what runs; and a method is already off by leaving its variables unset. No v0.1 app edited provider construction |
| `APIKeys(MaxTTL)` | **Kept as `APIKeyMaxTTL(d)`**: it narrows the range of the runtime setting `auth.api_key_max_ttl` to at most `d` and lowers its default to `d`, so operators can shorten it and never exceed the app's policy, and `/ops/settings` shows the real range |
| `WithoutRegistration` | **Kept.** `POST /v1/auth/register` isn't registered (404, absent from the document, so clients generated from it don't offer sign-up); a first Google, Apple or GitHub sign-in of an unknown address is refused with the new code `registration_closed` (403, or `#error=registration_closed`), before anything is written. Operators' accounts, verification, reset, linking and custom methods keep working. There are no invitations in `authhttp` yet (organisations are Phase 7) |
| `Prefix` | **Dropped.** Moving `/v1/auth` breaks the callback URLs registered at Google, Apple and GitHub (`/v1/auth/{provider}/callback`) and Apple's server notification URL, the stack's `CrossOrigin` exceptions for Apple's cross-site posts, the `auth_ip` rate limit on `/v1/auth/`, `SignInMethods`' callback details, the web and mobile client templates, and the frozen contract. An app that needs another path proxies it |
| `Brand` | **Kept**: logo, support address and footer (`mail.Brand`), with the app's name and `APP_PUBLIC_URL` as defaults; the dev console's previews use it |
| `Middleware` | **Kept as `RouteMiddleware`**, because `Authenticator.Middleware` already names the authentication middleware. It runs on the `/v1/auth/` operations only (a CAPTCHA, a country filter), after the stack and before sign-in's checks, through a route group; `/ops/auth/users` and `/ops/service-accounts` are unchanged |

### Hooks

| Hook | Runs | Its error |
|---|---|---|
| `BeforeLogin(ctx, tx, LoginAttempt)` | Every sign-in (password, passkey, Google, Apple, GitHub, `SignIn`) once every factor is verified, the second included, and after the ban check, in the transaction that creates the session; not for impersonation (development only) | `Refuse`: 403 with the code; anything else: 500. The transaction rolls back |
| `AfterLogin(ctx, LoginEvent)` | After the session is committed and audited | Logged; a panic is recovered and logged; the response waits at most 5 seconds, then the context is cancelled |
| `OnRegister(ctx, tx, NewAccount)` | In the transaction that inserts an account: registration (`password`), a first provider sign-in (`google`, `apple`, `github`, with the provider's name), `CreateUser` (`operator`) | Rolls the account back; the answer depends on the path (below) |

**When `BeforeLogin` runs.** Three placements were weighed:

| Placement | Verdict |
|---|---|
| Before credentials are checked | Rejected: the hook would run, and could answer, for unknown addresses and wrong passwords, becoming an enumeration and timing oracle, and it would see unauthenticated input |
| After the first factor, before the challenge | Rejected: a refusal would tell someone holding only the password something about the account that v0.1 shows only after the second factor (bans are checked when the session is created), and a hook could be tempted to decide on half-verified sign-ins |
| **After every factor and the ban check, in the session's transaction** | **Chosen**: the hook never sees a failed sign-in, so it adds no oracle and no timing difference; a refusal reaches only someone who passed every check; bans and second factors can't be skipped because both run first and the hook can only return an error; what it writes commits only with the session |

The hook gets the `pgx.Tx`: sign-in holds a transaction open anyway, and a second connection from `Deps.DB` would contend with it (and hooks are built in `main.go`, before `Deps` exist). `AfterLogin` has no transaction; it runs after the commit.

**What a client receives when `OnRegister` fails.** Email registration answers 202 whether or not the address has an account (AUTH-S-4), and hooks run only for new accounts, so any other answer would disclose that the address was free. The account is rolled back, the error logged, and the answer stays 202. Refusing bad input belongs to validation that runs for every request: `RegisterFields`' tags and `Resolve`, and `PasswordPolicy`. A first provider sign-in (identity proven by the provider) and an operator's creation (trusted) answer 403 with the refusal's code, or 500.

**Refusal codes.** `Refuse(code, detail)` panics on a code that isn't lowercase snake_case of 3–64 characters or is built in: every problem code of the v0.1.0 Full apps (their example modules' aside, checked against the frozen `surface.json` files), the generic code of each status, sign-in's mappings and the codes added since. The item suggested validating at `New`; `New` can't see the codes a hook returns at run time without making apps declare them twice, so the check moved to `Refuse`, and the documented pattern is a package variable, which fails when the program starts, before `New`. A `*Refusal` built by hand is checked again when a hook returns it, and a reserved code answers 500, so a hook can never impersonate `invalid_credentials` or `account_banned`.

**Tracing.** Spans `authhttp.BeforeLogin`, `authhttp.AfterLogin` and `authhttp.OnRegister`, with `gorbital.auth.method` and `gorbital.auth.refused`; a failing hook sets the span's error status. Hooks never receive passwords, tokens or codes: `LoginEvent` has the session's ID, not its token.

**Audit.** A refusal records `auth.login.failed` with `reason: refused` and the code (and `provider`, `new_account` for a refused provider sign-up); `registration_closed` records `reason: registration_closed`. `auth.login.succeeded` gains `method` for sign-ins that aren't a password or a passkey (providers already had it). No new audit action.

### Registration fields

`RegisterFields[T](save)` registers `POST /v1/auth/register` with the body `delivery.RegisterBody[T]`: v0.1's email and password, `T` in a field hidden from JSON, a `TransformSchema` adding `T`'s properties and required fields to the body schema, and an `UnmarshalJSON` decoding the one object twice (into the base fields and into `T`). Huma validates the raw body against the merged schema before decoding and calls `Resolve` on `*T` (it finds resolvers in nested fields), so `T`'s rules run for every request. `additionalProperties` stays `true`, so v0.1 clients sending extra properties keep working. The schema is named after `T` (`RegisterBodyRegistrationFields`); only apps that opt in see it.

| Option | Verdict |
|---|---|
| `reflect.StructOf` embedding `T` beside email and password | Rejected: `StructOf` can't create the unexported `_` field that carries `additionalProperties`, and panics on embedded types with methods unless they come first, which `Resolve` needs |
| A `SchemaProvider` returning a hand-built schema | Rejected: an inline schema loses the component and Huma's generation of `T`'s tags |
| `Fields any` on `NewAccount` with a type assertion in `OnRegister` | Rejected: untyped for the only hook that uses them |
| **A generic body with `TransformSchema` and `UnmarshalJSON`, and a typed `save` after the `OnRegister` hooks** | **Chosen** |

`FuzzRegisterBody` checks the decoding never panics and gives exactly what decoding the base fields and `T` separately gives, so fields can't change sign-in's values. Accounts created without registration (providers, operators) run `OnRegister` but not `save`; the guide and Shelfie chapter 6 show completing a profile later.

### Custom sign-in methods: how a module reaches sign-in

`SignIn` must return what `POST /v1/auth/login` returns (200 with a cookie or a token, or 202 with a challenge) and apply what it applies. `gorbital` must not import `authhttp`.

| Option | Verdict |
|---|---|
| `Deps.Auth`, an interface in `gorbital` set when the authenticator implements it | Rejected: the result is an HTTP response with sign-in's schema (`LoginResponse`, cookies, transports), which would have to move into `gorbital` or be `any`; every module would receive sign-in whether it uses it or not; and `Deps` gains a field only when two built-in modules need it (§ 2) |
| A lookup such as `authhttp.From(deps)` or a registry | Rejected: a service locator |
| **The module takes the `*authhttp.Authenticator` as an argument (`phonelogin.Module(auth, sender)`), and `main.go` passes the value it gives to `WithAuth`** | **Chosen**: the dependency is explicit and typed, visible in `main.go`, and costs nothing to modules that don't sign anyone in. `SignIn` and `User` fail with a plain error before `Setup`, so a misuse surfaces in the first test |

A module whose `Module` takes arguments isn't listed by `orb gen modules` (it recognises `func Module() gorbital.Module`), so `main.go` adds it on its own line; Phase 8's `orb doctor` must not report such a folder as missing from `modules.gen.go`. `Deps.Auth` in item 67 is replaced by this.

`SignIn(ctx, SignInRequest{UserID, Method, Transport})`: an unknown or deleted account answers 401 `invalid_credentials`; then the account's `auth_login` and `auth_login_address` limits (429), an unverified address (403 `email_not_verified`, as login refuses it), two-factor authentication (202), the ban (403), `BeforeLogin`, the session, `auth.login.succeeded` with `method`, `AfterLogin`. The method name is lowercase snake_case and can't be a built-in method's (`password`, `passkey`, `google`, …), so audit events can't be forged to look like another method. v0.1 has no new-device emails, so there is nothing more to apply. `SignIn` verifies nothing about the method: its documentation, the guide and Shelfie's chapter 7 list what a module must (a credential bound to the account, single-use short-lived codes stored hashed, attempt limits per credential and per client, the same answer whether an account exists, the user ID from the verified credential and never from the request).

**The method through a second factor.** `LoginMFA` must know how a sign-in started, for `BeforeLogin` (an app refusing phone sign-in for administrators must not be bypassed by an administrator with two-factor authentication) and for the audit event. v0.1's `auth_mfa_challenges` has no column for it, and a migration would break Phase 5's promise that a v0.1 database migrates as a no-op and would need the column before the code that reads it. Instead the challenge token of a sign-in that didn't start with a password is `<random>.<method>`, and the stored hash covers the whole string: changing or removing the method makes the challenge unknown (`invalid_mfa`, tested). Password challenges keep v0.1's token. Tokens stay opaque to clients and within the 256-character limit.

### Threat model: Phase 6

| Threat | Mitigation | Proven by |
|---|---|---|
| **A hook becomes an enumeration or timing oracle** | `BeforeLogin` runs only after every factor; unknown addresses, wrong passwords, unverified addresses, failed second factors and bans never reach it. `OnRegister` errors don't change registration's 202. `RegisterFields` validation and `PasswordPolicy` run for every request, before any lookup. Registration's response padding (`auth.DefaultMinResponseTime`, 300 ms) still covers the hooks, as long as they are fast (documented) | `TestBeforeLogin` (responses for an unknown address and a wrong password equal an app without the hook; the hook is never called), `TestOnRegister` (a failing hook answers as for an existing address), `TestRegisterFields` (422 for new and existing addresses) |
| **A hook skips a ban or the second factor** | Both are checked before `BeforeLogin`; hooks can only return an error, never create a session or change the attempt | `TestBeforeLogin` (banned: `account_banned`, hook not called), `TestHooksCantSkipTheSecondFactor`, `TestSignInCustomMethod` (202 then `login/mfa`) |
| **A hook fails open** | Any error that isn't a refusal fails the sign-in with 500; the cause is logged, never sent | `TestBeforeLogin` |
| **A refusal impersonates a built-in code** | `Refuse` panics on reserved codes; a hand-built `Refusal` with one answers 500 | `TestRefuse`, `TestRefusalCodesExcludeV010Codes`, `TestBeforeLogin` |
| **An account is half created** | `OnRegister` and `RegisterFields` run in the account's transaction for every creation path; an error rolls back the account, the app's rows, the audit event and the verification email | `TestOnRegister` (registration, operator, Google), `TestRegisterFields` |
| **`SignIn` signs in someone the module didn't verify** | By design `SignIn` trusts the caller; its contract, the guide and the example state what to verify; the method is audited by name; login's limits, verified address, bans and second factor still apply | `TestSignInCustomMethod`; Shelfie's `TestPhoneSignIn`, `TestPhoneCodesAreBounded` |
| **A second factor is finished under another method's name** | The method is inside the hashed challenge token | `TestSignInCustomMethod` (forged and stripped suffixes: `invalid_mfa`), `TestChallengeToken` |
| **Registration fields override sign-in's** | `T` can't name `email` or `password` (any case); decoding is checked against separate decoding | `TestInvalidOptions`, `FuzzRegisterBody` |
| **Pre-registration of someone's address plants profile data** | Hooks don't run again when an unverified address registers again, so the first registrant's fields stay; v0.1 already removes their password and methods when the owner proves the address. Documented: treat registration fields as unverified input the owner can edit, never for authorization | The guide; residual risk |
| **A slow hook exhausts connections or delays sign-in** | `BeforeLogin` holds the session's transaction: documented to stay a few queries, bounded by `APP_REQUEST_TIMEOUT`; `AfterLogin` bounded at 5 s (a hook that ignores its context keeps running in its goroutine, documented) | `TestAfterLogin` |
| **Secrets reach app code or traces** | Hook types carry no password, token or code; spans carry the method and the refusal code only | Type definitions; `TestHooksCantSkipTheSecondFactor` checks the event doesn't contain the token |
| **Options weaken defaults** | The password minimum only rises; the API key cap only narrows; TOTP and recovery codes can't be turned off; `RequireMFA` can't target `user` | `TestInvalidOptions`, `TestPasswordOptions`, `TestAPIKeyMaxTTL`, `TestRequireMFA` |
| **Sign-up without registration** | `WithoutRegistration` removes the route and refuses provider sign-ups before writing | `TestWithoutRegistration` (the native and web flows) |
| **Route middleware reaches operators' APIs** | `RouteMiddleware` is registered on a group for `/v1/auth/` paths only | `TestBrandAndRouteMiddleware` |

### Known gaps

- The documentation strings of the password fields still say "at least 12 characters" with `MinPasswordLength`; the 422 detail gives the real minimum.
- Service accounts aren't user accounts: `OnRegister` doesn't run for them.
- `AfterLogin` hooks that ignore their context outlive the response.

## Phase 7 implementation notes: organisations and tenancy (2026-09-17)

Full-multi's generated `internal/modules/orgs` moved into `gorbital.dev/gorbital/orgshttp`, with a guard for app modules scoped by organisation. Items 71–75 are in the [roadmap](../v0.2-roadmap.md#phase-7-organisations-and-tenancy). A v0.1 app, which doesn't link `orgshttp`, sees nothing.

### Layout and public surface

| Path | Holds |
|---|---|
| `orgshttp/orgshttp.go`, `module.go` | `Module(auth *authhttp.Authenticator, opts ...Option)`, `Option`, `Brand`; what full-multi's `internal/app` did for organisations: `module_orgs.go` (error mappings), `permissions.go` (`declareOrgPermissions` and the two platform permissions), `settings.go` (`orgs.*`), `job_orgs_purge.go`, `orgs_hooks.go`, `orgs_service_accounts.go`, the `deleted_organisations` retention row and the `orgs_invitations` limiter |
| `orgshttp/internal/{domain,usecase,repository,delivery}`, `internal/delivery/jobs/orgspurge` (`internal/jobs/orgspurge` until Phase 9) | The golden module's layers and job, unchanged except import paths, doc comments naming where declarations live, route registration (`operation.Register`) and the settings problems (below). The first commit of the phase copies them unchanged |
| `orgshttp/internal/repository/migrations` | `00001_orgs.sql` and `00002_settings_org_purge.sql`, byte for byte full-multi's, declared under `20260916000001` and `20260918000002` |

Added elsewhere:

| Where | Added | For |
|---|---|---|
| `gorbital` | `Permission.OrgRoles`, `Declarations.OrgPermissions`, `OrgGrants`; `Platform.OrgPermissions` (the organisation catalog, unfrozen) and `Platform.Authenticator`; `OrgAuthorizer` and `Platform.SetOrgAuthorizer` | Organisation permissions declared by any module; the guard's source of memberships |
| `guard` | `OrgMember(permission)` | Item 72 |
| `authhttp` | `Organisations`, `(*Authenticator).UseOrganisations`, `(*Authenticator).OrgServiceAccountRoutes` | Account hooks and organisations' service accounts (Phase 5 moved them but didn't mount them) |
| `gorbitaltest` | `(*App).SignUp`, `SignUpPassword` | Tests with real accounts, which organisations need (`org_members` references `auth_users`) |

### How organisations reach sign-in, and sign-in organisations

Sign-in and organisations depend on each other at run time: members are accounts, a new account gets a personal workspace, deleting an account checks and changes organisations, and organisations' service accounts are stored and authenticated by sign-in but managed by owners and admins. In v0.1 the app wired both directions (`orgsHooks`, `orgAccess`). `gorbital` imports neither module.

| Option | Verdict |
|---|---|
| `orgshttp.Module(opts...)` finding sign-in through `Platform` | Rejected: `Platform` isn't built when the OpenAPI document is exported, so the service account operations would be missing from the document; and a lookup hides the dependency |
| `authhttp` mounting organisations' service accounts when told by an option such as `authhttp.Organisations(orgs)` | Rejected: `main.go` would build and pass two values in two places, and `orgshttp` still needs sign-in for its foreign key and hooks |
| Relocating the service account operations into `orgshttp` | Rejected: they are sign-in's use cases, SQL and API keys; `orgshttp` can't import `authhttp/internal`, and duplicating them is what the phase must not do |
| **`orgshttp.Module(auth, opts...)` takes the authenticator, as Phase 6's modules do; `authhttp` exports the `Organisations` interface, `UseOrganisations` and `OrgServiceAccountRoutes`** | **Chosen**: explicit in `main.go`, typed, and the service account operations stay sign-in's code, registered in the organisations module's routes. `Module.Platform` checks `auth` is `Platform.Authenticator` (a configuration error otherwise), builds the use cases and calls `UseOrganisations`; sign-in's use cases get adapters that ask whatever is connected, so `Setup`'s order doesn't matter, and without organisations they behave as Phase 5's (hooks do nothing, organisation service accounts are 404) |

The roadmap's `orgshttp.Module(opts...)` became `Module(auth, opts...)` for these reasons.

### `guard.OrgMember`

| Decision | Why |
|---|---|
| The guard asks an `OrgAuthorizer` the app has one of, set by the organisations module from `Module.Platform`; `SetOrgAuthorizer` refuses a second one | A guard is built when routes are declared, without `Deps`; memberships and the organisation catalog are the organisations module's. A registry field resolved at registration (like rate limits) keeps guards plain route options |
| `route.Guard.Org` is resolved by the router instead of a `Check` function | A guard's `Check` returns only an error; this guard must also pass on a new context (`huma.WithContext`) acting in the organisation, with `postgres.WithOrg` |
| Semantics are `orgs.RequireMember`'s on members and the organisation's enabled service accounts (`Service.Memberships`), and the refusals are v0.1's problems with v0.1's details (`org_not_found` 404, `forbidden` and `mfa_required` 403), written by the guard | Identical answers to v0.1's use-case check; the guard doesn't depend on the module's error mappings being mounted |
| Malformed IDs answer `org_not_found` without asking the authorizer | They can't be anyone's organisation; timing reveals only that an ID is malformed, never whether a well-formed one exists |
| An authorizer returning a context that doesn't act in the path's organisation is a 500 | The guard can't let a buggy authorizer attach another organisation's scope to the connection |
| Registration fails for a path without `{orgId}` or a public route; `New` fails when routes use the guard and no module authorizes organisations | A misconfigured guard must not start, and must never answer as if a membership check had passed. Mounted by hand with `gorbital.Mount`, the guard answers 500 |
| The organisations module's own operations keep their use-case checks rather than the guard | `POST /v1/orgs/{orgId}/restore` works on deleted organisations, which `Memberships` doesn't return, and the other operations lock the organisation and check the role again under the lock (security review ORG-4); moving them would change v0.1's order of responses for nothing |
| `OrgRoles` on `gorbital.Permission`, declared in a second catalog (`Platform.OrgPermissions`) with owner, admin and member first and v0.1's descriptions | Generated modules declare their organisation permissions on their `Module`, as platform ones; v0.1's `orb:anchor org-permissions` edit of `permissions.go` has no place in an app on `gorbital.Main`. A permission can't have both `Roles` and `OrgRoles`, so an organisation role name never becomes a platform role |

### `orb gen module --org`

| Decision | Why |
|---|---|
| Routes under `/v1/orgs/{orgId}/<names>`, each with `guard.OrgMember` and the read or write permission; both permissions with `OrgRoles` owner, admin and member | As v0.1's organisation resources, whose org catalog gave every role read and write; apps narrow `OrgRoles` by hand |
| Use cases take the organisation from the path and check it is the actor's (`actor.OrgID`), answering 401 otherwise; every statement filters on or inserts `org_id`; records carry `OrgID` and `CreatedBy` | The guard decides who acts in the organisation, the queries which rows belong to it: a use case called without the guard (a job, another module) can't act in an organisation it wasn't authorized for |
| The foreign key to `orgs` with `ON DELETE CASCADE` is added in a `DO` block when `orgs` exists | A plain `REFERENCES orgs` failed the migrations of every test app built from `migrations.FS` without `orgshttp`, such as another module's tests. The purge still removes the rows (a generated test checks it); a database migrated before `orgshttp` was added gets the table without the key, and the generator's first next step says to add `orgshttp` before migrating |
| The `org_isolation` policy (enable, force, policy) ends the migration when the app has a `db/migrations/*_row_level_security.sql` migration or `rls: true` in `gorbital.yaml` | `orb add rls` needs a v0.1 lock; apps on `gorbital.Main` add that migration by hand, and later modules must be covered without another step |
| Generated tests use `gorbitaltest.App.SignUp` accounts and their personal workspaces: `org_not_found` on every route for non-members, unknown and malformed IDs, `<record>_not_found` across organisations, read-only API keys, pages, versions, audit events with `org_id`, purge | Organisation members are accounts; `gorbitaltest.User` principals aren't |
| The plan never edits `main.go`; when no non-test file of `cmd/api` mentions `orgshttp`, the next steps give the line | ADR-0021; the authenticator variable is the app's |
| Golden output: Shelfie's `clubbooks` module (`orb gen module ClubBook title:string:unique 'author:string?' 'status:enum(proposed,reading,finished)' note:text --org`), in `TestModuleMatchesShelfie`; `TestGeneratedCodePasses` generates organisation modules before and after a row-level security migration | As Phase 8's owner-scoped golden |

### Behaviour kept, and the differences

The golden app's organisation HTTP tests (organisations end to end, API keys and organisations, idempotency keys across organisations, organisations' service accounts, organisation settings and flags, row-level security) run against an app on `gorbital.New` with `authhttp`, `opshttp`, `flagshttp` and `orgshttp`, with the example projects module replaced by an organisation-scoped module guarded by `guard.OrgMember`. The golden use-case tests run against the moved layers. `gorbital/internal/integration` checks the multi-tenant app against full-multi's frozen v0.1.0 OpenAPI (compatible under `/v1/auth/`, `/ops/`, `/v1/flags`, `/v1/webhooks/resend`, `/v1/orgs` and `/v1/invitations`; each of the 156 operations and every schema they reference as in v0.1.0 but for `x-gorbital-guards`) and surface (error codes, audit actions, platform and organisation roles with v0.1's grants, settings, jobs, `/ops/retention`'s ten rows in order).

| Difference | Why |
|---|---|
| A request without credentials gets 401 before its body is parsed | Deny by default (ADR-0082), as for `/ops` in Phase 4 |
| The anonymous body schema of `POST /v1/orgs` is `CreateInputBody`, not `CreateInputBody1` | Huma numbers anonymous body schemas in registration order; in full-multi the example projects module registered `CreateInputBody` first. The name depends on the app's modules in v0.1 too; the content is identical |
| Organisation settings operations answer `setting_not_found`, `setting_version_conflict`, `setting_reason_required` and `invalid_setting_value` as problems of their own | v0.1 relied on the ops module's mappings; an app on `gorbital.Main` may not mount `opshttp`, and mapping the same errors twice fails `New` |
| `orgs_invitations` is listed in `/ops/auth/rate-limits` | v0.1 created the limiter without listing it; operators can now reset it |
| No preview of the invitation email in the dev console, no `seed` command | Previews are added through `AuthSetup`, which only the authenticator receives; `Main` has no seed command (Phase 5) |

### Threat model: Phase 7

| Threat | Mitigation | Proven by |
|---|---|---|
| **Cross-tenant access through path IDs**: a member of one organisation, their API key or a service account of their organisation sends another organisation's `{orgId}`, or a resource ID of another organisation under their own | Every operation under `/v1/orgs/{orgId}` checks membership on every request (the guard or `orgs.RequireMember`) before touching the organisation's rows, and answers exactly as for an organisation that doesn't exist; queries filter on `org_id` from the path the check authorized, so a resource ID of another organisation is `<resource>_not_found`; a service account's key reaches only the organisation its principal names | `TestEveryOrganisationRouteRefusesOtherOrganisations` (every `{orgId}` operation of the document, as another organisation's owner, API key and service account, compared with unknown and malformed IDs, and nothing changed in the target), `TestOrganisationsCantReachEachOthersServiceAccounts`, `…Settings`, `…Flags`, `TestOrgMember` |
| **Probing which organisations exist** | 404 `org_not_found` with one detail for deleted, unknown, malformed and foreign organisations; operations that need a session refuse API keys before looking at the organisation | `TestEveryOrganisationRouteRefusesOtherOrganisations` |
| **A guard that fails open** | Authorizer errors other than the four refusals are 500; a context not acting in the path's organisation is 500; no authorizer fails `New`, and by hand answers 500 | `TestOrgMember`, `TestOrgMemberNeedsOrganisations`, `TestOrgMemberWithoutAnApp` |
| **RLS bypass paths** | `postgres.WithoutRowLevelSecurity` needs a reason, is logged on its first connection and is called in one place outside tests, `postgres.Migrate`; requests, the guard, `orgshttp` and the purge never bypass (the purge deletes through `ON DELETE CASCADE`); the guard sets `postgres.WithOrg` for the handler's connections; startup warns about a role with `BYPASSRLS`, unforced tables and organisation tables without a policy | `TestRowLevelSecurityBypassesAreKnown` (every call in the repository), `TestRowLevelSecurity` (requests, settings, purge and migrations as a role without `BYPASSRLS`, a query without `org_id` seeing one organisation, a forged insert refused), `TestRowLevelSecurityCoversOrganisationTables`, `modules/postgres` `TestRowLevelSecurityFollowsTheContext` (the bypass logged once with its reason) and `TestWithoutRowLevelSecurityNeedsAReason` |
| **Invitation token handling** | 256 bits from `crypto/rand` in the URL fragment of the email only, never in responses, rows or audit events (SHA-256 hash, unique index, constant-time comparison under the lock); single use; valid while pending, for the invited verified address, and while the inviter may still give the role; resending replaces the token; every unusable token (unknown, used, revoked, expired, replaced, empty) gets the same 404 `invitation_not_found`; a forwarded link gets 403 only while pending, which reveals nothing to someone without the token; API keys can't accept | `TestInvitationTokens`, `TestOrganisationsEndToEnd`, `TestAPIKeysAndOrganisations`, the use-case tests |
| **Organisation service account key scoping** | A service account's key holds its role's permissions in its own organisation only, limited to the key's scopes, never an owner role or one requiring a second factor, never organisation management (`session_required`) or platform APIs; disabling or deleting the organisation stops its keys; purging removes the accounts and keys | `TestServiceAccountKeyScopes`, `TestOrganisationsCantReachEachOthersServiceAccounts` |
| **Role escalation** | Roles compared by permissions: nobody gives, changes or removes a role granting a permission their own role doesn't, only owners manage owners, the last owner stays, service accounts can't get owner or a role above the creator's; roles app modules add through `OrgRoles` follow the same rule; organisation roles never become platform roles | `TestRoleEscalation` (with a module's `billing` role), `TestOrganisationsEndToEnd`, the use-case review tests |
| **Schema drift in a released database** | Migrations byte for byte full-multi's under the same versions | `TestModuleMigrationsMatchV01App`, `TestMigrateOnV01DatabaseIsNoOp` |
| **The wrong authenticator connected** | `orgshttp`'s `Platform` refuses an `auth` that isn't `Platform.Authenticator` (exit 2) | Code review; `orgshttp.Module` documentation |

### Known gaps

- `orb add rls` needs a v0.1 multi-tenant app's `gorbital.lock`; apps on `gorbital.Main` add the policy migration by hand (the invoicing recipe shows it) until `orb new` writes the new layout (Phase 9).
- The dev console doesn't preview the invitation email; `Main` has no seed command.
- The organisations module's migrations keep v0.1's versions (`20260916000001`, `20260918000002`): a database of an app on `gorbital.Main` already migrated past them can't add `orgshttp` without recreating it or applying the two files by hand, since goose refuses out-of-order migrations. Allowing them would change `postgres.Migrate` for every app; documented instead, and `orb add orgs` now warns with both routes (organisations guide, Shelfie chapter 8).
- The Dev Portal's built UI (`cli/internal/portal/ui/dist`) still shows the module form's organisation option as disabled; the API accepts `org: true`. Needs a gorbital-dashboards build.
- `orgshttp` requires `authhttp`: an app with another authenticator (such as `modules/jwt`) has no built-in organisations.

## Phase 9 implementation notes: new apps on the v0.2 layout (2026-09-17)

Item 83 of the [roadmap](../v0.2-roadmap.md#phase-9-upgrade-eject-and-new-apps) and the Getting started pages of item 88: `orb new --preset full` writes §3's layout. Ejection (item 84) and the layout move of existing apps (item 85) are separate parts of the phase.

### Golden apps and templates

| Decision | Why |
|---|---|
| `examples/full-single` and `full-multi` are the golden apps on `gorbital.Main`; the v0.1-layout golden apps move, unchanged but for `go.mod`'s `replace` paths, to `examples/v0.1/` | v0.1 apps keep their layout (D2): orb still needs the v0.1-layout golden apps for their templates, for `orb gen resource`, `orb gen job` and `orb add mail` in those apps, and for the library's tests against v0.1 apps. They are byte for byte the v0.1.0 golden apps, so rebuilding a v0.1.0 app's files still proves every hash |
| Templates: `cli/internal/recipes/full` and `full-multi` stay the v0.1 layout's; the v0.2 layout's are `v0.2/full` and `v0.2/full-multi`. Minimal has one layout | A release's directories keep the layout they always held, so `orb upgrade` reads any release the same way (ADR-0050); Minimal keeps composing core packages (D17, the Phase 3 numbers are unchanged) |
| `gorbital.lock` records `"layout": "v0.2"` in its inputs; no layout is the v0.1 layout, so v0.1 locks are unchanged and stay readable by orb v0.1 | Rebuilding the base and rendering the new tree need the layout that wrote the app; guessing from the files would mistake a half-moved app |
| `main.go` builds the app in `options()`, which the app's tests pass to `gorbitaltest.New` | Tests and the dump of `internal/tools/refdocs` build the app exactly as it runs, without repeating `main.go` |
| The email provider is `cmd/api/mail.go` (`mailer`, for `gorbital.WithMailerFunc`, and `mailProvider`, for `opshttp.MailProvider`); `orb add mail` replaces it from `recipes/mail/<provider>.mail.go.tmpl`. File storage is `cmd/api/storage.go` (`fileStorage`, for `gorbital.WithStorageFunc`) | v0.1's `infra_mail.go` and `storage.go` had the same roles; production requires a provider and S3-compatible storage, so a new app must build them without editing `main.go` |
| **`WithStorageFunc`'s function may return a nil store to keep the built-in choice** (library change) | One function serves every `STORAGE_DRIVER`: the local driver keeps its signed links, which a store built by the app couldn't serve |
| The `projects` module is `orb gen module Project name:string:unique description:text 'status:enum(active,archived)'` (with `--org` in full-multi), checked by `TestModuleMatchesGoldenApps`; the migrations keep the v0.1 golden apps' versions (`20260915000002`, and `20260916000002` after the organisations module's, so the conditional foreign key to `orgs` is created) | The generator's output is the example; the versions match the apps that part C converts |
| `ping` is a module (`example.ping_message` setting, `example.ping_time` flag, public `GET /v1/ping` and `POST /v1/echo` with v0.1's operation IDs and schemas); the `heartbeat` example job is dropped | Settings and flags are declared by modules now; `orb gen job` writes v0.1 apps' `internal/app`, and a job belongs to a module's `Jobs` |
| No `cmd/migrate` or `cmd/seed`: `Main` serves `migrate`, and **`authhttp` adds a `seed [--email]` command** (library change): the administrator with a verified address, `platform_admin` and two-factor authentication, secrets printed once, development only, idempotent. `orb dev` runs `go run ./cmd/api seed` when `cmd/api` imports `authhttp` (or an ejected `internal/modules/auth`) | Rejected: a `WithCommands` option for an app-owned `seed` (sign-in's use cases are internal to `authhttp`, so the app couldn't create the administrator), and keeping `cmd/seed` (it would need the same internals). Example records aren't seeded: they are the app's modules' |
| Library text that contains the placeholder name, such as `opshttp`'s example instance host `acme-api-7d9f8-x2kq` in `api/openapi.json`, isn't templated in the v0.2 trees (`generate.KeepLibraryLiterals`) | The code that produces it is the library's, identical in every app; the frozen v0.1 trees keep templating it as v0.1.0 did |
| `api/openapi.baseline.json` is the v0.1 golden apps' released document, checked under `/ops/` by `TestOpsAPICompatible` in `cmd/api` | `/ops` is the library's now, but a v0.2 app keeps the same promise to its clients |

### The app's public surface: `api/surface.json`

In v0.1, `TestPublicSurface` recorded every name the app could return: its own and those of every `gorbital.dev` package it linked. When a library release added a name (`request_timeout` in `httpx`, [ADR-0085](0085-security-layers.md#package-moves-2026-09-17)), every existing app's test failed after `go get`. In the v0.2 layout the library's names come from the library, versioned and checked there, so:

| Option | Verdict |
|---|---|
| Record every name, as v0.1 | Rejected: a library upgrade that adds a code fails the app's tests, the incident the stability rule works around |
| Record every name, failing only on removals of library names and on the app's own additions | Rejected: listing the library's settings, jobs, roles and permissions needs a built app, so a database in a test that ran without one in v0.1; and a removal of a library name is caught before release by the library's own checks |
| **Record the app's own names only: what `modules.All()` declares (permissions and the roles they name, settings, flags, jobs, error mappings) and the error codes and audit actions written in the app's source. Fail on any addition not recorded and any removal** | **Chosen**. No database, no library names, so `go get` can't fail it. `internal/modules/surface_test.go`, recorded with `go test ./internal/modules -run TestPublicSurface -update`; a module `main.go` adds outside `modules.All()` is listed in its `surfaceModules` |

The library's names keep their promise through `gorbital/internal/integration` (`TestNamesKeepV010`, and its multi-tenant twin, against the frozen v0.1.0 surfaces), `docs/reference` (generated from the running golden apps and the source of every package they link) and `internal/tools/contracts`, which now finds each v0.1.0 name in a golden app's `api/surface.json` or in `docs/reference` (only the dropped `heartbeat` example is excused).

`Platform.Permissions`, the platform catalog, is added beside `OrgPermissions` (library change), so the refdocs dump reads roles, permissions and second-factor requirements from the running app.

### orb on the two layouts

| Command | v0.2 layout | v0.1 layout |
|---|---|---|
| `orb upgrade` | Merges the release's `v0.2/` tree | Merges the v0.1 tree this orb still carries; never writes v0.2 files. The report says `layout: v0.1` (`"layout"` in `--json`) and points at `orb upgrade --layout v0.2` |
| `orb add orgs` | Merges `v0.2/full` into `v0.2/full-multi` (`main.go` gains `orgshttp.Module(auth)`, the example module becomes the organisation one) and adds only the conversion migration: the organisation tables are the library module's | As before: the organisations migration copied under a new version, the conversion, the later organisation migrations |
| `orb add mail` | Replaces `cmd/api/mail.go`, the `.env.example` block, `gorbital.yaml` and the lock | Unchanged |
| `orb add storage`, `orb add rls` | Work: an app on `gorbital.Main` now has a lock (Phase 7's gap) | Unchanged |
| `orb gen resource` | Runs `orb gen module`; in a multi-tenant app records belong to organisations by default, as in v0.1 | Unchanged |
| `orb gen job` | Refuses (exit 2) and points at `Module.Jobs` | Unchanged |
| `orb dev` | `go run ./cmd/api migrate`, `go run ./cmd/api seed` | `cmd/migrate`, `cmd/seed` |
| New migration versions (`orb gen migration`, `orb gen module`, `orb add rls`, `orb add orgs`) | At least one after the newest built-in migration (`latestBuiltinMigration`, checked against the library by a test), since the app's `db/migrations` doesn't hold them: the library's versions run ahead of the calendar, and goose refuses a migration older than the database's newest | The app's files hold the library's copies, as before |
| `orb doctor`'s row-level security check | `migrate --status --json` of `gorbital.Main` now reports `row_level_security` problems as v0.1's did (library change) | Unchanged |

The Dev Portal's module form offers `--org` (gorbital-dashboards `framework/phase-9-templates`, synced into orb).

### Known gaps

- `orb add orgs` in a v0.2 single-tenant app can't migrate an existing database: the organisations module's migrations keep v0.1's versions (Phase 7), older than those a database already ran, and goose refuses them. `orb add orgs` warns (`migration_order_warning` in `--json`) and names both routes: recreate a development database, or apply the two files and record them in `goose_db_version` by hand ([organisations guide](../start/organisations.md#adding-organisations-to-a-database-that-already-exists)). An opt-in `--allow-out-of-order` on the app's `migrate` command (`goose.WithAllowOutofOrder`) stays open; it would change `postgres.Migrate`'s API.
- v0.1's `api maintenance on|off` command has no counterpart on `Main`; maintenance mode is changed through `/ops/settings`.
- `orb gen job` doesn't generate module jobs. The Dev Portal's job screen reads the app's jobs from `internal/app/job_*.go` in a v0.1 app and, since Phase 9's part C, from the `jobs.Define` calls of `internal/modules` in an app on `gorbital.Main`, with the name each call names; the runtime list has always come from `/ops/jobs`, so the module jobs of a Main app were never missing from the screen, only their source. Editing a generated job as a form stays a v0.1-layout feature.
- An app's `TestPublicSurface` no longer notices a library name disappearing; the library's checks do.
- `orb new` offers no way to create a v0.1-layout app.
## Phase 9 implementation notes: orb eject (2026-09-17)

`orb eject <auth|flags|mailevents|ops|orgs>` (roadmap item 84) implements §7. The command was removed in v0.3.0 ([ADR-0092](0092-what-the-framework-owns.md)); what an app's own copy of a module is, and how it is maintained, is now [The code in your repo](../guides/the-code-in-your-repo.md).

### What is copied, and where

| Decision | Why |
|---|---|
| The package is read from the directory `go list -m -json gorbital.dev/gorbital` reports in the app (the module cache, downloaded with `go mod download` when missing, or a `replace` directive's directory), not from `go.mod`'s version string alone | The app's build is what the copy must match, local checkouts included |
| `<package>/internal/<layer>/…` becomes `internal/modules/<module>/<layer>/…`; any other directory under `internal/` refuses the ejection | An app module has the four layers of §3 below it, and the app's architecture test enforces it; `internal/modules` is internal to the app already |
| The package keeps its name (`package authhttp` in `internal/modules/auth`), and imports of it get that name explicitly | Every call site in the app, `main.go`'s options and hooks included, stays as it is, so the only edit to the app is an import path. Renaming the package to the directory would rewrite every file using it, and `flags`, `orgs` and `auth` collide with `gorbital.dev/modules/{flags,orgs,auth}`, which the modules import |
| Tests are copied, except files starting with `//orb:noeject <reason>` | Contract tests read the gorbital repository (the frozen v0.1.0 contracts, the golden apps' migrations, row-level security SQL, every Go file); they prove the library, not the app. Helpers other tests use were moved out of them. The command lists what it left out |
| Imports of modules the lock records as ejected are mapped too, in the copy and in the app's files (ejecting `auth` rewrites the ejected `orgs`' imports) | Ejected modules use each other's copies, in either order the app allows |

### gorbital internals the modules imported

An app can't import `gorbital.dev/gorbital/internal/...`. Each import was resolved so the built-in modules depend on public API only, rather than copying internals into apps or refusing:

| Import | Used for | Resolution |
|---|---|---|
| `internal/operation` (opshttp, flagshttp, mailevents, orgshttp) | Registering v0.1 `huma.Operation` declarations on a `Router` | Made public as `gorbital.dev/gorbital/operation` (`Register`). It uses public API only, and `orb upgrade --layout v0.2` can turn `huma.Register(api, op, h)` into `operation.Register(r, op, h)` mechanically |
| `internal/route` (authhttp's `delivery/routes.go`) | `route.Config.ActorCheckedByHandler`: sign-in's signed-in routes left the actor check to their use cases, after input validation (v0.1's order of responses) | Replaced by the public route option `gorbital.AuthenticateAfterInput()`, which the **router** enforces after validation, just before the handler, with the same 401 problem. It fails closed where the old field relied on every use case checking; registration refuses it with `guard.Public` or guards (which run before parsing). Sign-in's contract tests, HTTP tests and the frozen OpenAPI comparison pass unchanged |
| `internal/builtinjobs` (opshttp's wiring) | The retention job's name for `/ops/retention` | `gorbital.RetentionJob`, a literal checked against `builtinjobs.Retention` by a test |
| `internal/opstest` (opshttp, flagshttp, mailevents tests) | A test app with bearer tokens and the golden app's example module | Each module keeps the part its tests use in `testapp_test.go`: three copies of test code, so each module's tests are self-contained. A public test kit for built-in modules was rejected: it would be API for the library's own tests |

`authhttp/internal/{jobs,migrations,signintest}` and `orgshttp/internal/{jobs,migrations}` moved under layers (`delivery/jobs/…`, `delivery/signintest`, `repository/migrations`). `gorbital/internal/ejectable` keeps it so: every file eject copies imports no gorbital internal, every internal package is under a layer with an app's layer rules, `//orb:noeject` is on test files only with a reason, and the modules' tests compile without the marked files (`go test -overlay`).

### main.go and the rest of the app

| Option | Verdict |
|---|---|
| Rewrite the constructor call (`authhttp.New(...)` → `auth.New(...)`) with go/ast | Rejected: the package name stays, so the call needs no change; other files use the package too (Shelfie's `phonelogin` and `profiles` take `*authhttp.Authenticator`, `signin.go` builds its options, tests build apps) |
| **Rewrite the import path in every Go file of the app that imports the package, by editing the import spec's byte range found with go/parser, then gofmt** | **Chosen**: everything else stays byte for byte, and the change reads as one line per file in the plan's diff |

What `orb eject` can't follow is refused with instructions rather than guessed: no call to `gorbital.Main` in `cmd/api`, the package not imported by `cmd/api` (nothing to eject), imported as `_` or `.`, or never called through its constructor (`authhttp.New`, `<package>.Module`).

`orgshttp.Module` takes `*authhttp.Authenticator`, so an app using the library's `orgshttp` can't use an ejected sign-in's authenticator. `orb eject` asks `go list -deps` which library packages the app builds import the module and refuses, naming `orb eject orgs` to run first. The check is generic: it follows imports, not a list.

### Migrations, the module list and the architecture test

- Migrations the module declares with `gorbital.Migration` literals (read with go/ast, including elements of a `[]gorbital.Migration` literal) are copied to `db/migrations/<version>_<name>.sql` byte for byte, which §6's merge treats as the declared migration. The copied module keeps declaring them: its own tests migrate test databases from its embedded files, and removing the declaration would edit code for no behaviour change. A version already used by another file in `db/migrations` refuses the ejection; an identical file is noted. An app without `db/migrations` gets a note.
- `orb gen modules` (so `orb dev` and `go generate`) skips directories the lock records as ejected: `flagshttp` and `mailevents` declare `func Module() gorbital.Module` and would otherwise be listed twice, and moving them from `main.go` into `modules.gen.go` would change the modules' order (settings, permission catalogs, `/ops` listings). `orb doctor`'s `modules` check skips them too.
- The generated architecture test said "modules never import each other". Modules that take the authenticator import sign-in's root package, which becomes an app module when ejected. The rule is now: a module may import another module's root package, never its layers; the template and the example apps' copies changed. Alternatives rejected: reading `gorbital.lock` from the test (a test coupled to orb's file), and keeping ejected code outside `internal/modules` (against §7).

### gorbital.lock, orb doctor and orb upgrade

- After the files are written, `go mod tidy` runs and the app's `api/surface.json` is recorded again: the copied module's error codes and audit actions are the app's own names, which its `TestPublicSurface` compares (found by ejecting into the golden apps, which have that test; the example apps don't).
- The lock gains `ejected`: `module`, `package`, `version`, `date` and `sha256`, a hash of the package's source as copied (paths and contents, `//orb:noeject` files included). An app without a lock (apps on `gorbital.Main` not created by `orb new`) gets one with only `ejected`; reading such a lock skips the inputs check, and `orb upgrade` refuses it. Entries are validated (known module, its package, once each). `orb upgrade` and `orb add orgs` keep the entries when they rewrite the lock.
- `orb doctor`'s `ejected` check hashes the package at the version the app requires now (`go list -m`): equal is ok, different a warning with up to three changelog entries of later versions that name the package (`CHANGELOG.md` of `gorbital.dev` at the version the app requires), a missing directory a failure. A hash rather than a version comparison also notices changes from a local checkout.
- `orb upgrade` never changes an ejected module's files; merging library fixes into the copy is the app's.

### Exit codes and output

0 when ejected (or planned, with `--dry-run`); 1 for every refusal and failure, and when `go mod tidy` fails after the files are written (the error says so); 2 for an unknown module, more than one, or none with `--no-input` or without a terminal; 130 when cancelled at the confirmation. `--json` is documented in the CLI reference and recorded in `testdata/json/eject.json`.

### Proven by

`TestEjectedModulesPass` ejects `flags`, `ops`, `orgs` and `auth` into a copy of Shelfie, `flags`, `mailevents`, `ops` and `auth` into a copy of the golden app `examples/full-single`, `orgs` and `auth` into `examples/full-multi`, `orgs` into the invoicing recipe, and `mailevents` into the admin tool with the module added: after each, gofmt, `go build`, `go vet`, the exported OpenAPI, Postman collection and `llms.txt` identical to the committed ones, `orb doctor` ok for `modules`, `gorbital.lock` and each `ejected`, and a second ejection refused; then golangci-lint and the app's whole test suite with the ejected modules' tests. `TestEjectDryRunWritesNothing`, `TestEjectRefusals`, `TestEjectRefusesOtherApps` (v0.1 layout, no `gorbital.Main`, a `main.go` it can't follow), `TestDoctorEjectedModules`, `TestRewriteGoImports` and `TestChangelogMentions` cover the rest.

### Known gaps

- Going back to the library is manual (documented in the guide); so is ejecting several modules in one plan.
- Doc comments in the copy still name library paths (`gorbital.dev/gorbital/authhttp`), and sign-in's tracer keeps its name.
- The organisations module's row-level security tests aren't copied (they run full-multi's SQL from the repository); the app's own tests cover its policy.
- The Dev Portal has no eject form.
- A lock `orb eject` creates has an empty `inputs` object.
- `orb upgrade --layout v0.2` (item 85) must record modules it keeps from a v0.1 app's generated code the same way; see the functions named in the roadmap notes.

## Phase 9 implementation notes: orb upgrade --layout v0.2 (2026-09-17)

Item 85 of the [roadmap](../v0.2-roadmap.md#phase-9-upgrade-eject-and-new-apps) moves an existing v0.1 app onto this layout. Guides: [Upgrading apps](../start/upgrading.md#move-to-the-v02-layout), [upgrade notes](../guides/upgrade-notes.md#moving-a-v01-app-to-the-v02-layout), recipe [Upgrading a v0.1 app](../examples/recipes/upgrading-a-v0.1-app.md); command: [CLI](../guides/cli.md#orb-upgrade---layout-v02).

### What the move compares

| Decision | Why |
|---|---|
| Three trees: the app, **this orb's v0.1 templates** rendered with the lock's inputs (what orb wrote), and **this orb's v0.2 templates** for the same inputs (what a new app has). The move refuses an app whose lock doesn't match the first, naming `orb upgrade` | Telling the developer's code from generated code is the whole job; an app one release behind would have changes the move would read as the developer's. Requiring plain `orb upgrade` first also keeps the move free of release history: it never fetches an older release |
| Every decision is per path, and the plan is complete before anything is written (`--dry-run` prints it) | A half-converted app is worse than an unconverted one; a conversion that stops at the first surprise leaves one |
| Nothing is committed, and a dirty tree is refused unless `--allow-dirty` | `git diff` is the review, `git restore` the undo. A commit orb writes would hide both, and the move touches hundreds of files |

### What happens to each file

| The app's file | The move |
|---|---|
| Generated code, unchanged, that the library now runs (`internal/modules/{auth,ops,flags,mailevents,orgs}`, `internal/app`, `internal/jobs`, `cmd/migrate`, `cmd/seed`) | Deleted; the report says which library package runs it |
| A built-in module with changes | Kept as the app's code: `copyEjectedModule` and `ejectMigrations` (the same functions `orb eject` uses) copy the library's module, and each change of the app's generated copy is placed in it (below). `gorbital.lock` gains the `ejected` entry `orb eject` would write |
| One of the app's own modules (`orb gen resource`, or written by hand) | Converted where it is (below) |
| An example module (`ping`, `projects`) nobody changed whose wiring can't be translated | Written from the v0.2 templates, with a report note: their operation IDs and paths are v0.1's, but the request schemas `orb gen module` writes are named after their operations and refuse unknown properties |
| A generated file the developer changed that the v0.2 layout doesn't have | A `manual` line: it stops the move, unless `--allow-manual` keeps the file under `_upgrade-v0.1/` (a directory the go command ignores) and converts the rest |
| `db/migrations` | Untouched, copies of the library's migrations included (below) |
| `api/openapi.json`, the Postman collection, `llms.txt`, `api/surface.json` | Regenerated from the converted app, so `git diff` shows what the move changed in them |
| Everything else (README, Dockerfile, `compose.yaml`, `.env.example`, `gorbital.yaml`) | Merged like `orb upgrade`, with conflict markers where both sides changed the same lines |

### Carrying a change into the library's module

The generated modules of v0.1 and the library's packages are the same code with other import paths (Phases 4–7 moved them unchanged), so a change to a v0.1 module is placed in the library's file by its **surrounding lines**: the lines of the change, with up to three lines of context on each side, must appear exactly once in the copy. The context shrinks (3, 2, 1, 0) and may be one-sided, so an insertion next to a line the library changed still lands. A change that can't be placed is reported with its diff, and the module isn't converted — the alternative, a copy with the change dropped or in the wrong place, would be a silent behaviour change in sign-in.

The report names the [options and hooks](../guides/configuring-sign-in.md) that could replace a change, by the file it touched (`usecase/register.go` → `OnRegister`, `RegisterFields`, `WithoutRegistration`; `usecase/password.go` → `PasswordPolicy`, `MinPasswordLength`; and so on), so an app can give the module back to the library. An app that owns sign-in also owns organisations, because the library's `orgshttp` takes `*authhttp.Authenticator`.

### Converting the app's own modules

| Step | How |
|---|---|
| `huma.Register(api, op, handler)` → `gorbital.Get/Post/Put/Patch/Delete(r, path, handler, options...)` | go/ast, on byte ranges: `OperationID`, `Summary`, `Description`, `Tags`, `Errors` and `DefaultStatus` become route options. v0.1's `signedIn` wrapper is read as assignments to the operation's fields (including `op.Errors = append([]int{401, 403}, op.Errors...)`) and merged into the literal; the bearer requirement becomes the router's deny-by-default, and an operation without security becomes `guard.Public()` |
| An operation the verbs can't express (`Responses`, `MaxBodyBytes`, `SkipValidateBody`) | `operation.Register(r, op, handler)`, the escape hatch this ADR added in Phase 9's first part; the report lists those operations |
| An operation with a field neither carries (`Hidden`, `Middlewares`, …) | The module isn't converted, and the field is named: carrying it over would change the operation |
| `func Register(api huma.API, …)` | Becomes `func Register(r *gorbital.Router, …)`, in every function of the module that takes the API and passes it on. Any other use of the API stops the conversion |
| The module's `Module` type (v0.1's `New`/`Register` wiring) | Renamed to `Wiring`, so the package can declare `func Module() gorbital.Module`, which the generated module list calls |
| `internal/app/module_<name>.go` | Its `mapper.Add` mappings become `Module.Errors`, its `resourcePermissions` become `Module.Permissions` (with `Roles: ["user"]`, or `OrgRoles` for an organisation resource), and the rest of the wiring function becomes the `Routes` closure, with `svc.db`, `svc.recorder` and `svc.logger` rewritten to `d.DB`, `d.Audit` and `d.Logger` and its `return err` to `panic(err)`, which `gorbital.Mount` reports naming the module |
| Anything else the wiring used (another `services` field, a value the composition root passed in, a name `internal/app` declared, the app's mapper outside `mapper.Add`) | Reported precisely, and the module isn't converted |

Permission checks stay in the use cases: moving them into `guard.Permission` would answer 403 before the input is validated, which is a change to responses the move can't prove is wanted. The router's deny-by-default does change one thing, which the report says: a request without a token gets 401 before its body is validated, where v0.1 validated first.

### The app's middleware

`internal/app/routes.go` is read as its two middleware lists (the literal and the `append` inside `if svc.auth != nil`), and compared with the templates' with the lists and comments cut out: any other change to the file is a `manual` line. The app's own steps are then run from `main.go`:

- added only after every built-in step, none removed → `gorbital.WithMiddleware(…)`, which runs there;
- otherwise `cmd/api/stack.go`, a `gorbital.WithStack` function listing the built-in steps in v0.1's order (with `Timeout` after `AccessLog`, as `Stack.Default` has it) and the app's steps where they were.

The files declaring that middleware move from `internal/app` into `cmd/api` as `package main`, with a header saying where they came from; a file that needs anything else of `internal/app` stops the move instead.

### Migrations

The app's `db/migrations` is left exactly as it is, the copies of the library's migrations included. §6's merge reads a copy with the same version and identical content as the same migration, so the database sees nothing new, and the app's own tests keep migrating from `migrations.FS` (a v0.1 module's repository tests do, and its migration references `auth_users`). Deleting the copies, which would make the tree look like a new app's, was rejected: it breaks those tests and buys nothing the report can't say ("delete them when nothing else reads them").

### Proven by

`TestLayoutMoveOnV01Apps` (roadmap item 87, `ORB_E2E=1` and a test database) moves apps created by the published orb v0.1.0 — or, without the module proxy, by this orb's v0.1 templates, which are that release's byte for byte — and checks each afterwards: gofmt, `go vet`, `go test ./...`, the OpenAPI document compatible with the pre-move one through `openapi.CheckCompatible`, `orb doctor` without failures, and `migrate --status` reporting nothing pending on a database the v0.1 app had migrated. Its cases are the untouched single- and multi-tenant apps, a changed sign-in module (kept, with the change in the library's file), middleware of the app's own (carried into `WithStack`), and a generated resource with a hand-written module (both converted and routed). `TestLayoutMovePlansAnUntouchedApp`, `TestLayoutMoveDryRunWritesNothing`, `TestLayoutMoveStopsAtChangesItCantMake`, `TestLayoutMoveRefusals`, `TestRewriteModuleRoutes`, `TestTransplant` and `testdata/json/upgrade-layout.json` cover the rest without a database. The user's own apps, `portal-demo` and `copper-lantern`, are converted on their `v0.2-layout` branches.

### Known gaps

- Settings, feature flags and jobs an app declared in `internal/app` (`orb gen job`'s output included) are reported, not moved: they belong in a module's `Settings`, `Flags` or `Jobs`, and which module is the developer's choice.
- A module whose permissions are organisation-scoped in a v0.1 multi-tenant app authorises through the organisations module's catalog and memberships, which the library's `orgshttp` doesn't expose; such a module is reported and left for the developer (the library's `guard.OrgMember` replaces it).
- The app's HTTP tests in `internal/app` aren't ported to `gorbitaltest`; they are kept under `_upgrade-v0.1/` as a follow-up.
- An example module that falls back to the v0.2 templates changes its request schemas' names and refuses unknown properties, which the report says and the developer can undo with git.
- Going back to the library after a module is kept is manual, as with `orb eject`.

## Why

- A `Module` value keeps each module's declarations next to its code, and removes the four edits of v0.1.
- Built-ins inside the composition module respect the dependency direction without new release units.
- A version table in the library is the only way to stop copying migrations without touching existing databases.

## Trade-offs

- The composition module's `go.mod` requires what every built-in needs (WebAuthn, OAuth), so `go.sum` lists more; binaries still contain only imported packages, and the dependency count is budgeted in [benchmarks](../benchmarks.md).
- The version table for v0.1 library migrations is a frozen list the library carries forever.
- An ejected module stops receiving fixes, by definition; `orb upgrade` names library security fixes that touch it.

## Consequences

- ADR-0022's layout remains valid for v0.1 apps; ADR-0039's resource template becomes `orb gen module` (layered is the only layout, so there is no `--layered` flag; Phase 8).
- ADR-0050's upgrades gain an opt-in conversion to this layout (Phase 9).
- Library modules keep their embedded `Migrations` variables for apps that compose them by hand.

## Implementation

[v0.2 roadmap](../v0.2-roadmap.md): module and deps in Phase 1, stack, authenticator and migrations in Phase 3, built-ins in Phases 4–7, generators in Phase 8, ejection in Phase 9.
