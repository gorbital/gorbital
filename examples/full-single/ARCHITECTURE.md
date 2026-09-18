# Architecture

This app runs on `gorbital.Main` (gorbital's v0.2 layout, ADR-0083). The library builds and runs the server: configuration, the middleware stack, `/ops`, jobs, email, audit, migrations and the commands. The app's own code is `main.go`, its modules and its migrations; sign-in is among its modules, copied from the library when the app was created. The module rules below are checked by `internal/modules/architecture_test.go`, so `go test ./...` fails when they are broken.

## Layout

```text
cmd/api/main.go          gorbital.Main: what the app contains, one line each (options())
cmd/api/mail.go          the email provider for MAIL_DELIVERY=provider (replaced by `orb add mail`)
cmd/api/storage.go       S3-compatible file storage for STORAGE_DRIVER other than local
cmd/api/*_test.go        the app as main.go builds it: end to end, api/openapi.json current, /ops compatible
db/migrations/           the app's own goose migrations, embedded as migrations.FS
internal/modules/
  modules.gen.go         the module list, func All (orb gen modules; don't edit)
  architecture_test.go   the layer rules below
  surface_test.go        the app's public names, recorded in api/surface.json
  <name>/                one module per directory, in four layers
    module.go            func Module() gorbital.Module: name, error codes, permissions, settings, flags, routes
    domain/              types, rules and errors: standard library only
    usecase/             service.go and ports.go, then one file per operation
    repository/          store.go, then one file per SQL statement (hand-written SQL)
    delivery/            routes.go (the route table and its guards), responses.go, then one file per operation
    <name>_test.go       HTTP tests through the real stack (gorbitaltest)
internal/modules/projects/ example module owned by the signed-in user, as orb gen module writes it
internal/modules/ping/   example module: a public endpoint, a runtime setting and a feature flag
internal/modules/auth/   sign-in, copied from gorbital.dev/gorbital/authhttp by orb new: package authhttp, its layers and tests
api/openapi.json         exported API contract (committed; review changes in pull requests)
api/surface.json         the app's error codes, audit actions, permissions, settings, jobs and flags: public names that may only grow
api/openapi.baseline.json the released /ops contract that TestOpsAPICompatible checks against
compose.yaml             PostgreSQL for development and tests; Grafana with orb dev --observability
```

What `main.go` adds, and where it lives:

| Line | What | Where |
|---|---|---|
| `gorbital.WithAuth(authhttp.New())` | Sign-up, sign-in, sessions, two-factor authentication, passkeys, Google, Apple and GitHub, API keys, service accounts, platform roles; the commands `roles`, `grant-role`, `revoke-role`, `reset-mfa`, `rotate-auth-keys`, `auth-providers` and `seed` | `internal/modules/auth`, the app's copy of `gorbital.dev/gorbital/authhttp`; options and hooks such as `MinPasswordLength`, `BeforeLogin` or `OnRegister` go in `authhttp.New(...)` |
| `opshttp.Module(...)` | `/ops/`: runtime settings, feature flags, jobs, the audit log, email, users, releases, observability and incidents, retention, storage | `gorbital.dev/gorbital/opshttp` |
| `flagshttp.Module()` | `GET /v1/flags`: the client feature flags of the signed-in caller | `gorbital.dev/gorbital/flagshttp` |
| `mailevents.Module()` | `POST /v1/webhooks/resend`: bounces and complaints, feeding the suppression list | `gorbital.dev/gorbital/mailevents` |
| `modules.All()` | The app's modules | `internal/modules` |
| `WithMigrations(migrations.FS)` | The app's migrations, run in one history with the library's | `db/migrations` |
| `WithMailerFunc(mailer)`, `WithStorageFunc(fileStorage)` | The email provider and file storage | `cmd/api/mail.go`, `cmd/api/storage.go` |

Sign-in is already your code: `orb new` copied it from the library version `go.mod` requires, with the migrations into `db/migrations` under the library's versions, and `gorbital.lock` records where from. Library releases no longer change it; `orb doctor` says when the library's copy has changed since, quoting the changelog. To change another built-in module beyond its options and hooks, `orb eject <module>` copies it the same way.

## Request flow

```text
HTTP → gorbital's stack (recover, trusted proxies, request ID, tracing, request counts, access log, timeout, security
       headers, CORS, cross-origin protection, body limit, maintenance, authentication, sign-in rate limit, idempotency
       keys) → the module's middleware → the route's guards → delivery → usecase → domain
     ← domain errors mapped to problem+json by the module's Errors
```

Every route requires a signed-in caller unless its route says `guard.Public()`. Permissions are checked by `guard.Permission` in `delivery/routes.go`, before the body is read; `orb routes` lists every route with its guards.

## Configuration

- **Environment** ([.env.example](.env.example)): secrets and infrastructure, read by `gorbital.LoadConfig`. Changing them needs a restart.
- **Runtime settings**: non-secret tunables a module declares in `Module.Settings`, stored in PostgreSQL, changed with `PUT /ops/settings/{key}`, applied on every instance within moments. The module keeps the `*settings.Setting` it declared and calls `Get` each time.
- **Feature flags**: declared in `Module.Flags`, turned on for organisations, users or a stable percentage with `PUT /ops/flags/{key}` (always with a reason). Flags declared `flags.Client()` are listed to signed-in clients by `GET /v1/flags`.
- **Job definitions**: declared in `Module.Jobs` with their code defaults (enabled, schedule, timeout, retries, queue), overridable with `PUT /ops/jobs/definitions/{name}`. Changing a job's code still needs a deploy.

A value is never in more than one layer, and secrets are never runtime settings.

## Business modules

`internal/modules/projects` is exactly what `orb gen module Project name:string:unique description:text 'status:enum(active,archived)'` creates. Generate your own modules the same way, or copy it. All of it is your code: change any rule, query or response.

- **Ownership:** every project has an `owner_id` (the signed-in user). Every repository method takes the owner ID, and someone else's project returns 404 `project_not_found`, so IDs can't be probed.
- **Permissions:** `projects.project.read` and `.write`, held by every user through the `user` role and by an API key only when its scopes include them.
- **Lists:** `GET /v1/projects` uses keyset pagination through `gorbital.dev/page`: `limit`, an opaque `cursor`, and `sort` by one allowlisted field, with one fixed query per sort in `repository/select_projects.go`.
- **Updates:** `PATCH` sends the `version` it read; a stale version returns 409 `project_version_conflict` instead of overwriting someone else's change.
- **Audit:** `projects.project.created`, `.updated` (changed field names only) and `.deleted`.
- **Tests:** domain rules, and HTTP tests through the real stack including cross-owner access, in `projects_test.go`.

## Migrations

`go run ./cmd/api migrate` applies gorbital's modules' migrations and the app's in one goose history, ordered by version, then River's job tables. Migrations never run at startup; run them before starting a new version. Never edit a released migration: add one with `orb gen migration <name>`.

## Email

Modules send email through `Deps.Mailer`: it fills the sender from the `mail.*` runtime settings and queues the message; the mail worker delivers it with retries, skipping addresses on the suppression list. In development it goes to orb dev's mail catcher; with `MAIL_DELIVERY=provider` (always in production) through the provider in `cmd/api/mail.go`. `orb add mail` replaces that file and the `# orb:begin mail` block of `.env.example` to switch between Resend and SMTP; don't edit them by hand.

## Rules

1. `domain/` imports only the standard library. Domain types have no struct tags.
2. `usecase/` imports its own `domain/` and never `delivery/` or `repository/`; it declares the ports repositories implement in `ports.go`.
3. `repository/` and `delivery/` import their module's `domain/` and `usecase/`; `delivery/` never imports `repository/`. `module.go` wires the layers.
4. Modules never import other modules' layers. What one needs from another goes through an interface wired in `main.go`, or the other module's root package, as a module that takes sign-in's authenticator imports `auth`'s `authhttp`.
5. Only `gorbital.LoadConfig` reads environment variables; modules get what they need through `gorbital.Deps`, their settings and their flags.
6. Handlers return domain errors unchanged; the module's `Errors` map them to an HTTP status and a stable error code.
7. Request body types tolerate unknown fields (`additionalProperties:"true"`), so older servers accept newer clients.
8. Error codes, audit actions, permissions, setting keys, flag keys and job names are public API: add new ones, never rename them. `api/surface.json` records them.

## Adding an endpoint

1. Add the rule and its error to `domain/`.
2. Add the use case to `usecase/`, in its own file.
3. Add the input, output and handler in their own file in `delivery/`, and the route with its guards in `delivery/routes.go`.
4. Map new domain errors in the module's `Errors` in `module.go`.
5. Run `go test ./...`, `go test ./internal/modules -run TestPublicSurface -update` and `go run ./cmd/api openapi --dir api`.
