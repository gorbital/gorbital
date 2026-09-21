# Changelog

Notable changes to the gorbital library, the `orb` CLI and generated apps. The library modules and `orb` are versioned together. What existing apps must do for each release is in the [upgrade notes](docs/guides/upgrade-notes.md); why things changed is in the [decision records](docs/adr/README.md).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). `v0.1.0` is the first public release. Until `v1.0.0` there is no compatibility promise between minor versions ([ADR-0015](docs/adr/0015-public-api-and-stability-tiers.md)), though the compatibility checks already run; breaking changes are listed here and in the upgrade notes.

## v0.3.1 (2026-09-21)

### Fixed

- **`gorbital.dev/gorbital@v0.3.0` does not build for anyone who downloads it.** Twelve modules' `go.mod` still required their siblings at `v0.2.1`, and the `replace` directives that make this repository work do not apply to a consumer. Only `gorbital.dev/gorbital` actually broke — it is the one that calls a symbol this release added to a sibling — so it resolved `gorbital.dev/modules/postgres v0.2.1` and failed on `undefined: postgres.WithScope`. The other twenty modules build at `v0.3.0`. Every sibling requirement now names `v0.3.0`, and the whole set is retagged `v0.3.1` so one version resolves across all of it. Use `v0.3.1`; `v0.3.0` cannot be repaired, because release tags are immutable.
- The release runbook gains the check that would have caught it: `go get` the module into an empty module and build it, before any tag is pushed.

## v0.3.0 (2026-09-21)

Tenancy becomes a contract your app fills in, sign-in becomes a choice, and a generated resource states its access rule instead of assuming one. The library is additive; the CLI removes two things.

### Tenancy is yours to name

#### Added

- `gorbital.Scope` and `gorbital.ScopeAuthorizer`: the framework's tenancy is now five pieces of data your app supplies — the concept's name, the path parameter its ID arrives in, the code a refusal carries, whether an ID is well formed, and its roles — plus a one-method authorizer that decides membership ([Tenancy](docs/guides/tenancy.md), [ADR-0088](docs/adr/0088-scope-tenancy-as-a-contract.md)). Your API can say `/v1/merchants/{merchantId}/orders` and refuse with `merchant_not_found`. **The framework never learns your table names and never queries them**; membership lives behind the authorizer, in one function you write.
- `gorbital.WithScope`, `Platform.SetScope`, `gorbital.ErrScopeNotFound`, `gorbital.ScopeRole`, `gorbital.ScopeGrants`, `Permission.ScopeRoles`, `guard.Scope` and `postgres.WithScope`.
- `orgshttp.DefaultScope` supplies the organisation vocabulary, and `orgshttp.ScopeName` mounts the same organisations module under your own words: it changes the paths, the path parameter, the refusal code and the OpenAPI tag, and deliberately changes no table, migration, permission or role name.
- `orgshttp.Module` takes an `Identity` interface instead of `*authhttp.Authenticator`, so organisations work with whatever authenticates the app. `orgshttp.WithoutServiceAccounts` is for an identity that cannot issue API keys.

#### Changed

- `guard.OrgMember`, `OrgAuthorizer`, `Platform.SetOrgAuthorizer` and `Permission.OrgRoles` are the older names of the four above. They keep working for all of v0.x and carry no `Deprecated` marker, so `staticcheck` does not fail the build of an app that already uses them.
- `actor.Actor.OrgID` and the row-level-security session setting `gorbital.org_id` keep their names. Both now mean *the scope*; renaming either would change what already-stored audit rows and live policies mean.

### Sign-in is a choice

#### Added

- `authhttp.Methods` gates sign-in by method: password (13 operations), operators (16), TOTP (5), passkeys (9), social (12) and API keys (19 including organisation service accounts). A disabled method registers no routes, declares no settings, jobs, limiters or permissions, and appears in no OpenAPI document; its paths answer 404, never 401 ([Sign-in profiles](docs/adr/0089-sign-in-profiles.md)).

#### Unchanged on purpose

- A method that is off keeps its migrations. The tables exist and stay empty, so turning the method on later is a line in `main.go` and not a migration applied out of order.

### A resource states its access rule

#### Added

- `orb gen module --scope user|tenant|public|custom` ([Resource access](docs/guides/resource-access.md), [ADR-0091](docs/adr/0091-resource-access-policies.md)). `custom` writes a `policy.go` into the module as your file, with `CanRead`, `CanWrite` and `Filter` returning `gorbital.ErrNotImplemented`, which the module maps to 501: an unwritten rule refuses rather than serving every row. The generator adds no implicit tenant filter and no tenant column.
- `orb routes` gains a scope column. `orb doctor` names `tenant` modules whose generated repository queries have lost the tenant column, and `custom` modules whose policy is unimplemented — a static check over generated files, which the output says.
- Generated isolation tests for `user` and `tenant`, in your app's own vocabulary.

#### Changed

- `--scope org` is the older name of `--scope tenant` and still works. `--json` reports the canonical `tenant`.

### A copy that knows what it is missing

Since v0.2.1 `orb new` writes sign-in and organisations into the app as the app's own code, so a fix in the library does not reach them with `go get`. This is the other half of that trade ([ADR-0092 §7](docs/adr/0092-what-the-framework-owns.md)).

#### Added

- **`orb doctor --security`** ([The code in your repo](docs/guides/the-code-in-your-repo.md#orb-doctor---security)). For each module `gorbital.lock` records as copied from the library, it fetches the version the copy was made from and reports: whether that version's source still hashes to what the lock recorded, how many of the files your app holds changed upstream and which of them, how many you have changed yourself so that porting is a merge rather than a copy, the files the library has gained since, the changelog entries naming the package by release, and the `diff -ru` to run. It then runs seven static rules over your own Go syntax trees and the SQL in `db/migrations`: `credential-stored-unhashed`, `password-without-kdf`, `secret-compared-directly`, `weak-random-secret`, `scope-query-without-soft-delete`, `credential-in-route-path` and `secret-in-log`, each with the file, the line, why it matters, the fix and a guide. `--json` like the rest of `orb doctor`; exit code 1 when something failed.
- **It cannot tell you whether a change upstream is a security fix**, and says so. gorbital publishes no advisory feed, so there are no advisory IDs and no severities; the sign-in flow it may name beside a changed file is read from the file's name and labelled a hint. Every run ends by listing what it checked and what it did not, because a clean run is not an assurance. No finding prints a value it found — only where it is and what it is called.

### Removed

- **`orb eject`.** `orb new` has written sign-in and organisations into the app since v0.2.1, so the command answers a question nobody has ([ADR-0092](docs/adr/0092-what-the-framework-owns.md)). The name stays registered for this release and explains itself rather than failing as an unknown command; the copy machinery underneath still runs for `orb new`, `orb add orgs` and `orb upgrade --layout v0.2`.
- **`orb new --no-eject`.** There was one shape of Full app and one flag that asked for a second; now there is only the first.
- `opshttp`, `flagshttp` and `mailevents` can no longer be copied into an app. They are plumbing, not business rules.

### The example applications moved

- The showcase applications live in [github.com/gorbital/examples](https://github.com/gorbital/examples), tagged in step with the library ([ADR-0093](docs/adr/0093-the-examples-repository.md)). Their history moved with them and this repository's CI no longer builds them.
- `examples/minimal`, `examples/full-single`, `examples/full-multi`, `examples/v0.1/*` and `examples/shelfie` stay: they are not documentation but the source the CLI's templates are generated from and compared against.
- A page includes their code by marker as before; `scripts/examples.sh` fetches the ref `docs/examples.json` pins, and `docscheck` fails when it is missing or stale.

### Documentation

#### Added

- [Tenancy](docs/guides/tenancy.md), [Access control](docs/guides/access-control.md) and [Resource access](docs/guides/resource-access.md).
- ADRs [0088](docs/adr/0088-scope-tenancy-as-a-contract.md), [0089](docs/adr/0089-sign-in-profiles.md), [0090](docs/adr/0090-composing-presets.md), [0091](docs/adr/0091-resource-access-policies.md), [0092](docs/adr/0092-what-the-framework-owns.md), [0093](docs/adr/0093-the-examples-repository.md), and the [v0.3.0 roadmap](docs/v0.3-roadmap.md).

#### Fixed

- `refdocs` recorded new identifiers as arriving in `v0.2.0`, frozen two releases ago.

## v0.2.1 (2026-09-18)

A new app now carries its own sign-in and organisations. Existing apps are unchanged.

### Sign-in and organisations in your repository

#### Changed

- `orb new --preset full` puts the whole sign-in module in the app, at `internal/modules/auth`, and with `--tenancy multi` the whole organisations module too, at `internal/modules/orgs` — every file, every test and every migration, in `db/migrations`. You can read exactly what registration, login, email verification, password reset, sessions, two-factor authentication and the organisation process do, and change them. It is what `orb eject` already did, now done for you when the app is created ([The code in your repo](docs/guides/the-code-in-your-repo.md)).
- The HTTP API, the database and the behaviour are the same as an app that imports the modules from the library.
- What moves is the flows — handlers, use cases, repositories and migrations. The primitives they call stay in the library: Argon2id password hashing, session tokens, TOTP, passkey and OAuth/OIDC verification (`gorbital.dev/modules/auth`) and organisation authorisation (`gorbital.dev/modules/orgs`), so fixes to those still reach you with `go get`. A fix to a flow you own does not: `orb doctor` reports each module the app owns and warns when the library's version of it has changed, quoting the changelog.
- `orb new --no-eject` keeps sign-in and organisations in the library, as in v0.2.0.
- The example apps that use sign-in — Plateful, Shelfie, invoicing and the internal admin tool — carry their own sign-in and organisations the same way.

### Documentation

#### Added

- **Build an app**, a new documentation tab: 23 chapters that build [Plateful](docs/build/00-what-were-building.md), a multi-tenant restaurant delivery platform, from `orb new` to deployment — what the framework gives you and what you write, customising and extending it, and where it stops helping. Its code is included from `examples/apps/plateful`, which CI builds and tests.
- `examples/apps/plateful`: one platform, many restaurants, each an organisation, with platform staff, restaurant staff, and customers and couriers who belong to no organisation. Eight modules: restaurants, menus, orders, couriers, payments, reviews, images and notifications. A restaurant is its organisation: its fields are columns on the organisation table, not a second table.
- A documentation tab can carry a highlight dot (`"highlight": true` on the tab in `docs/docs.json`).

#### Fixed

- The site header announced v0.1.0.
- The upgrade notes pinned `@v0.2.0`; install commands use `@latest`.
- Two commands in the CLI guide named `go run ./cmd/migrate`, which a v0.2 app does not have.

### The CLI

#### Fixed

- `orb dev` refuses to start the Dev Portal when `APP_ENV` is `production`, as its documentation had said; `--no-portal` runs the app without it. The loopback, token and request-header checks are unchanged.
- After `orb gen migration`, `orb gen resource` and `orb add mail`, the Dev Portal and the printed next steps name the migrate command the app's layout uses — `go run ./cmd/api migrate` in a v0.2 app — instead of always `go run ./cmd/migrate`.

### Quality

#### Fixed

- `TestIncidentDetectionThroughJob` read the incident's audit event without waiting for the job to record it, and failed on a loaded runner. It waits now, in the library and in the v0.1 templates. Because a v0.1 template changed, `orb upgrade --layout v0.2` asks an app created by orb v0.1.0 to run `orb upgrade` first — the order the upgrade notes already give.
- CI gives the test jobs a 40-minute timeout (the CLI package alone runs for about ten, against Go's ten-minute default), and the end-to-end job fetches the release tags `orb upgrade` reads.

## v0.2.0 (2026-09-18)

v0.2 turns gorbital into a framework apps import: routes, guards, the middleware stack, the application itself, sign-in, the operations API and organisations move from generated code into the library ([roadmap](docs/v0.2-roadmap.md), [ADR-0081](docs/adr/0081-a-framework-you-import.md)). It is additive: apps created with `v0.1.0` keep building and passing their tests against it with `go get` and no code changes, moving to the new layout is an opt-in `orb upgrade --layout v0.2`, and any built-in module can be copied back into the app with `orb eject`. What to do for each change is in the [upgrade notes](docs/guides/upgrade-notes.md#upgrading-to-v020).

New decision records for the line: [ADR-0081](docs/adr/0081-a-framework-you-import.md) *A framework you import*, [ADR-0082](docs/adr/0082-routes-guards-and-middleware.md) *Routes, guards and middleware*, [ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md) *Modules, the default stack, migrations and ejection*, [ADR-0084](docs/adr/0084-versioned-documentation.md) *Versioned documentation*, [ADR-0085](docs/adr/0085-security-layers.md) *Security layers*, [ADR-0086](docs/adr/0086-dev-portal-tunnel.md) *Dev Portal tunnel* and [ADR-0087](docs/adr/0087-testing-sign-in-from-the-dev-portal.md) *Testing sign-in from the Dev Portal*.

### Modules, routes, guards and middleware

#### Added

- `gorbital.dev/gorbital`, the composition module ([ADR-0082](docs/adr/0082-routes-guards-and-middleware.md), [ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md)): `Module`, `Permission` and `Deps`; `Declare` and `Grants` for permissions, runtime settings and feature flags; `Mount` for error mappings and routes; `Get`, `Post`, `Put`, `Patch` and `Delete` on router groups with the options `Summary`, `Description`, `Tags`, `OperationID`, `Status`, `Errors` and `Deprecated`. Every route requires an authenticated actor unless it has `guard.Public()`; the check runs before the input is parsed and answers 401 `unauthenticated`. Registration mistakes are errors naming the module. Guide: [Modules and routes](docs/guides/modules-and-routes.md).
- Guards and route middleware ([ADR-0082](docs/adr/0082-routes-guards-and-middleware.md)): `guard.Permission`, `guard.RecentReauth` (new error code `reauthentication_required`), `guard.RateLimit` with `ByUser`, `ByAPIKey`, `ByIP` and `Named` (shared across instances through `Deps.RateLimits`), and custom guards with `guard.New`. Guards run before input parsing, document their responses and `x-gorbital-guards` in OpenAPI, and their refusals are counted in `gorbital.guard.refusals`. `gorbital.Use` and `Module.Middleware` attach any `func(http.Handler) http.Handler` to routes, groups and modules. Guide: [Guards and middleware](docs/guides/guards-and-middleware.md).
- `gorbital.Customize`: change a route's Huma operation for what the other options don't set, such as a streaming response's media types; the method, path, operation ID, security and middleware can't change.
- `gorbital.dev/gorbital/operation`: `Register` registers a v0.1 `huma.Operation` declaration on a `gorbital.Router`, as the built-in modules do. Public, so ejected modules and code moved from v0.1 apps can use it.
- `gorbital.AuthenticateAfterInput`: a route option that checks the authenticated actor after input validation instead of before parsing, keeping the route's sign-in requirement. Sign-in's routes use it for v0.1's order of responses, now enforced by the router rather than left to the use cases.
- `httpx.Capture` and `httpx.Captured`: record the status, headers sent and size of a response, for middleware that reads it.

### The app: configuration, lifecycle and the test kit

#### Added

- The app in the library ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#phase-3-implementation-notes-2026-09-17)): `gorbital.LoadConfig` reads every environment variable of a v0.1 Full app, with the same defaults and production refusals, and reports every problem at once; `gorbital.New` builds what a v0.1 app's `internal/app` builds (telemetry, the pool, audit log, settings and flags, email delivery, shared rate limits, idempotency keys, file storage, request metrics, the built-in jobs, release tracking, the dev console, routes and the middleware stack), and `(*App).Run` serves it with v0.1's shutdown order; `Handler`, `Deps` and `Close`. `gorbital.Main` serves the commands `serve`, `migrate [--status [--json]]`, `migrate-down`, `openapi [--dir]` and `version`, with exit codes 0, 1 and 2, and the commands built-in modules contribute (`Command`, `ErrUsage`). Options `WithName`, `WithModules`, `WithAuth` (the `Authenticator` interface, with optional `Module` and `Commands`), `WithStorage`, `WithStorageFunc`, `WithMailer`, `WithMailerFunc`, `WithMiddleware`, `WithMiddlewareFunc`, `WithStack` (the built-in steps as `Stack`, with a startup warning when `Recover` or `Auth` is left out), `WithLogger` and `WithMigrations`. `Module` gains `Jobs` and `Migrations`. Guides: [Your main.go](docs/guides/main-go.md), [The middleware stack](docs/guides/middleware-stack.md), [environment variables](docs/guides/environment-variables.md#apps-on-gorbitalmain).
- `gorbital.Migrate`: one goose history of the library's migrations (served under the versions v0.1 apps hold their copies under, from a frozen table), built-in modules' `Module.Migrations` and the app's `db/migrations`, then River's. Identical copies collapse, conflicting content fails naming both files, and a database migrated by a v0.1 app applies nothing.
- `httpx.Maintenance` and `httpx.MaintenanceOptions`: maintenance mode as core middleware, moved from the Full apps' `maintenance.go`, with the open paths passed in.
- `gorbital.WithStorageFunc`: a nil store with a nil error keeps the built-in storage (the local driver for `STORAGE_DRIVER=local`).
- `gorbital.Platform.Permissions`: the platform permission catalog.
- `gorbital.dev/gorbital/gorbitaltest`: an app per test on its own migrated database, requests through the whole stack as `User` or `APIKey` principals, `AssertStatus`, `AssertProblem`, and the mail and jobs the app queued. `NewWithEnv` sets the test app's environment variables, such as `AUTH_ENCRYPTION_KEYS`; `(*App).Config` returns its configuration, for running commands against the test's database; `(*App).SignUp` and `SignUpPassword` create real, verified accounts signed in with a bearer token. Guide: [Testing with gorbitaltest](docs/guides/testing-with-gorbitaltest.md).

### Sign-in in the library

#### Added

- `gorbital.dev/gorbital/authhttp`: sign-in for apps on `gorbital.Main`, added with `gorbital.WithAuth(authhttp.New())`. `New` returns an `Authenticator` with `Middleware`, `Module`, `Commands`, `CheckConfig` and `Setup`. It is v0.1's generated auth module moved into the library unchanged: the 74 operations under `/v1/auth/*`, `/ops/auth/users/*` and `/ops/service-accounts/*`, use cases, SQL, the `auth_cleanup` and `auth_revoke_tokens` jobs, and the migrations under the same versions, so a database migrated by a v0.1 app migrates as a no-op. Endpoints, bodies, error codes, audit actions, permissions, roles, `auth.*` settings, rate limiter names, cookies and environment variables are unchanged, checked by contract tests against the v0.1.0 OpenAPI. `Main` serves `roles`, `grant-role`, `revoke-role`, `reset-mfa`, `rotate-auth-keys`, `auth-providers` and `seed [--email]` (v0.1's `cmd/seed`: the development administrator with two-factor authentication) with v0.1's output; wrong arguments exit with status 2. Guides: [Authentication](docs/guides/authentication.md#in-an-app-on-gorbitalmain), [Your main.go](docs/guides/main-go.md#sign-in-commands), [environment variables](docs/guides/environment-variables.md#apps-on-gorbitalmain).
- `gorbital.AuthSetup`: an authenticator passed to `WithAuth` may have `Setup(ctx, AuthSetup)`, which `New` calls with the configuration, `Deps` and the permission catalog, and `Main` before the authenticator's commands, and `CheckConfig(Config)`, whose error is a configuration error (exit status 2). `AuthSetup.Handle` serves routes outside the OpenAPI document, such as the passkey `/.well-known` files; `AuthSetup.MailPreviews` adds emails to the dev console's previews; `AuthSetup.DevEndpoints` serves endpoints behind the dev console's checks. Apps on `Main` get the dev console's dev operator on `/ops/` and previews of sign-in's emails.
- Sign-in options, hooks and custom methods ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#phase-6-implementation-notes-sign-in-options-hooks-and-custom-methods-2026-09-17)). `New(opts ...Option)`: without options it is v0.1's sign-in, and the frozen v0.1.0 OpenAPI of `/v1/auth/*` is unchanged. Invalid options are reported by `CheckConfig` (exit status 2) and `Setup`.
  - Options: `MinPasswordLength` (12 to 128), `PasswordPolicy` (422 `weak_password`), `RequireMFA` for roles modules declare, `APIKeyMaxTTL` (narrows `auth.api_key_max_ttl`), `WithoutRegistration` (`POST /v1/auth/register` not served; a first Google, Apple or GitHub sign-in of an unknown address gets the new code `registration_closed`, 403), `Brand` (`mail.Brand` for sign-in's emails and their previews) and `RouteMiddleware` (middleware on `/v1/auth/`).
  - Hooks: `BeforeLogin`, in the session's transaction once every factor, the second factor and the ban check have passed, refusing with `Refuse(code, detail)` (403, audited as `auth.login.failed` with `reason` `refused`) and failing closed on other errors; `AfterLogin`, after the session is committed, with errors and panics logged and a 5-second limit; `OnRegister`, in the account's transaction for email registration, first provider sign-ins and operators' accounts. `LoginAttempt`, `LoginEvent`, `NewAccount`, `Refusal`, `User`. Traced as `authhttp.BeforeLogin`, `authhttp.AfterLogin` and `authhttp.OnRegister`. Refusal codes can't be sign-in's or gorbital's own.
  - `RegisterFields[T]`: fields of the app's own on `POST /v1/auth/register`, validated by Huma from `T`'s tags and `Resolve` before anything is stored, shown in OpenAPI, and saved in the account's transaction.
  - `(*Authenticator).SignIn(ctx, SignInRequest)` and `SignedIn`: a module's own sign-in method gets login's results (rate limits, verified address, the second-factor challenge, bans, hooks, audit with the method, cookie or bearer transport). `(*Authenticator).User` and `ErrUserNotFound`. A module receives the authenticator as a constructor argument; there is no `Deps.Auth`, and `gorbital` doesn't import `authhttp`.
  - Dropped from the roadmap: `Prefix`, code options for Google, Apple, GitHub and passkeys (they stay in environment variables), and `MFA(TOTP, RecoveryCodes)` toggles (they would weaken defaults).
  - Guides: [Configuring sign-in](docs/guides/configuring-sign-in.md), [Sign-in hooks](docs/guides/sign-in-hooks.md), [Extra registration fields](docs/guides/extra-registration-fields.md), [Adding a sign-in method](docs/guides/adding-a-sign-in-method.md).

#### Changed

- Challenge tokens of second-factor sign-ins that started with Google, Apple, GitHub or a module's method end in `.` and the method, such as `.google`; a password sign-in's token is v0.1's. They stay opaque to clients, but are longer.

### Operations, flags and email events in the library

#### Added

- Built-in modules moved from the Full apps' generated `internal/modules/{ops,flags,mailevents}` with the same paths, operation IDs, schemas, error codes, permissions, roles and audit actions, checked against the frozen v0.1.0 contracts ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#phase-4-implementation-notes-2026-09-17), guide: [Ops API](docs/guides/ops-api.md#adding-it-to-an-app)):
  - `gorbital.dev/gorbital/opshttp`: `Module`, with the option `MailProvider`. `OPS_ALLOWED_IPS` (`Config.OpsAllowedIPs`) limits `/ops/` to client addresses after `APP_TRUSTED_PROXIES`, answering 403 `ip_not_allowed` before the sign-in check. In development the dev console's token operates `/ops/` as the `dev-console` system actor with `platform_admin`'s permissions, in the stack's `Auth` step.
  - `gorbital.dev/gorbital/flagshttp`: `Module` (`GET /v1/flags`) and `PermRead`.
  - `gorbital.dev/gorbital/mailevents`: `Module` (`POST /v1/webhooks/resend`), which checks `RESEND_WEBHOOK_SECRET` when the app starts.
- What the built-in modules read from the whole app: `Module.RateLimiters` and `Module.Retention` (`RateLimiter`, `Retention`) collect the named rate limiters and retention policies from modules instead of listing them in the app, the built-in `retention` job (`gorbital.RetentionJob`) deletes what modules declare with a `Delete` function, and `/ops/auth/rate-limits` also lists the limiters `guard.RateLimit` creates. `Module.Platform` gives built-in modules `Platform` (the configuration, job manager, health checks, instance, `RateLimiters`, `Retention`, `Authenticate` and `OnShutdown`); an error from it is a configuration error.
- Sign-in and the operations API together, as in a v0.1 app, with nothing more in `main.go` ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#integration-of-phases-4-and-5-2026-09-17)): `/ops/auth/providers` lists the authenticator's sign-in methods (`gorbital.SignInMethod`, `Platform.SignInMethods`, and `authhttp.Authenticator.SignInMethods`), `authhttp` declares its rate limiters (`auth_login` to `auth_api_key`) in `/ops/auth/rate-limits` and the `deleted_accounts` and `unverified_accounts` rows of `/ops/retention`, and `ops.auth.read` is declared once, by `authhttp`. Tested end to end with real sign-in in `gorbital/internal/integration`, with the whole app's OpenAPI and public names checked against the v0.1.0 contracts.
- `migrate --status --json` of `gorbital.Main` reports `row_level_security` problems, as v0.1's `cmd/migrate` does, for `orb doctor`; the text form prints them as warnings.

### Organisations

#### Added

- Organisations for apps on `gorbital.Main` ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#phase-7-implementation-notes-organisations-and-tenancy-2026-09-17); guides: [Organisations](docs/start/organisations.md#in-an-app-on-gorbitalmain), [Row-level security](docs/guides/row-level-security.md#in-an-app-on-gorbitalmain)):
  - `gorbital.dev/gorbital/orgshttp`: `Module(auth, opts...)` with the option `Brand`. Full-multi's generated organisations module moved into the library: organisations, members and roles, invitations, personal workspaces, deletion, restore and the `orgs_purge` job, organisation settings and flags, with v0.1's paths under `/v1/orgs` and `/v1/invitations`, operation IDs, schemas, error codes, audit actions, permissions, roles, `orgs.*` settings and migrations (a database migrated by a v0.1 multi-tenant app migrates as a no-op). It takes the app's `*authhttp.Authenticator`, and mounts organisations' service accounts under `/v1/orgs/{orgId}/service-accounts`. `/ops/retention` lists `deleted_organisations`, `/ops/auth/rate-limits` the `orgs_invitations` limiter.
  - `guard.OrgMember(permission)`: for routes under `/v1/orgs/{orgId}/`, checks on every request that the caller (a user, their API key within its scopes, or the organisation's service account) is a member whose role grants the permission; 404 `org_not_found` for every organisation the caller isn't in, 403 `forbidden` and `mfa_required`. The actor then acts in the organisation and the request's connections carry it for row-level security (`postgres.WithOrg`). Documented in OpenAPI and `x-gorbital-guards` as `org_member:<permission>`.
  - `gorbital.Permission.OrgRoles`, `Declarations.OrgPermissions` and `OrgGrants`: organisation permissions a module declares, in an organisation catalog with the roles `owner`, `admin`, `member` and any role a module names (`Platform.OrgPermissions`). `OrgAuthorizer`, `Platform.SetOrgAuthorizer` and `Platform.Authenticator` for built-in modules.
  - `authhttp.Organisations`, `(*Authenticator).UseOrganisations` and `(*Authenticator).OrgServiceAccountRoutes`, which `orgshttp` uses for personal workspaces, account deletion and service accounts.
  - Contract tests: an app with `authhttp`, `opshttp`, `flagshttp`, `mailevents` and `orgshttp` is compatible with full-multi's frozen v0.1.0 OpenAPI, and has its names; full-multi's organisation HTTP tests run against the library, with a threat model's tests for cross-organisation IDs, row-level security bypasses, invitation tokens, service account keys and role escalation.

### Security layers

#### Added

- New layers, each opt-in ([ADR-0085](docs/adr/0085-security-layers.md), guide: [Security layers](docs/guides/security-layers.md)):
  - `gorbital.dev/httpx/timeout`: `timeout.New(d)` gives each request a context deadline and, when the handler hasn't started its response, answers 503 with the new error code `request_timeout`; later writes are discarded, streaming and `http.ResponseController` keep working. Apps on `gorbital.Main` get it as the `Timeout` step of the default stack (`APP_REQUEST_TIMEOUT`, default 30s), and `gorbital.Timeout(d)` shortens it for a route.
  - `gorbital.dev/httpx/ipfilter`: `ipfilter.New(allow, deny)` and `ipfilter.ParsePrefixes` refuse requests by client address after `httpx.TrustedProxies` with the new error code `ip_not_allowed` (403); `ipfilter.ErrDenyAll`.
  - `gorbital.dev/webhook`: the `Verifier` interface, `NewHMAC` for HMAC-SHA256 senders (GitHub, Shopify, Slack) and `NewStandard` for Standard Webhooks and Svix (Resend, Clerk), with secret rotation, a 5-minute replay window and constant-time comparison. `guard.Webhook` and `guard.WebhookBodyLimit` verify a webhook's signature before the route's input is parsed; 401 `invalid_webhook_signature`, 413 `request_too_large`.
  - `gorbital.dev/modules/jwt`: authenticate tokens from external identity providers (Auth0, Clerk, Supabase, Firebase, Cognito) with cached JWKS, rate-limited key refresh, algorithm, issuer, audience and time checks, and claims mapped to the actor; invalid tokens get the new error code `invalid_token` (401).
  - `timeout` and `ipfilter` are packages of their own rather than part of `httpx`: apps generated by `orb` v0.1.0 record the problem codes of every `gorbital.dev` package they link in `api/surface.json`, so a new code in `httpx` failed their `TestPublicSurface` ([stability](docs/guides/stability.md#adding-error-codes)).

#### Changed

- The dev console (`/_dev/*`) and the development operator on `/ops/` refuse requests carrying forwarding headers (`Forwarded`, `X-Forwarded-For`, `X-Forwarded-Host`, `X-Real-IP`, `True-Client-IP`, `CF-Connecting-IP`, `CF-Ray`, `CDN-Loop`), so a tunnel or proxy on the developer's machine, which connects from loopback, can't reach them ([ADR-0086](docs/adr/0086-dev-portal-tunnel.md)). `orb dev` removes `CLOUDFLARE_TUNNEL_TOKEN` and `CLOUDFLARE_TUNNEL_TOKEN_FILE` from the app's environment.
- `resend.VerifyWebhook` uses `gorbital.dev/webhook`; its API, errors and behaviour are unchanged.

#### Fixed

- `mail.RedactAddresses` missed addresses with a combining mark before `@` (such as a decomposed accent), which could reach logs unredacted. Found by fuzzing.
- `auth.NormalizeRecoveryCode` wasn't idempotent for codes with separators next to whitespace other than spaces; generated and typed codes normalize as before, so stored hashes still match. Found by fuzzing.

### Generators and the CLI

#### Added

- Generators and commands for apps on `gorbital.Main` ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#phase-8-implementation-notes-2026-09-17), guide: [Generating code](docs/guides/generating-code.md), Shelfie [chapter 9](docs/examples/shelfie/09-generators.md)):
  - `orb gen module <Name> <field:type>...` writes a layered module with one file per operation in `usecase/`, `repository/` and `delivery/`, the route table with `guard.Permission`, error mappings and permissions on the `Module`, keyset pagination and versioned updates, gorbitaltest HTTP tests, domain tests, a migration, the architecture test when missing, and `modules.gen.go`. Fields as `orb gen resource`, plus optional strings (`name:string?`); `--org` generates a module owned by organisations (routes under `/v1/orgs/{orgId}/…` with `guard.OrgMember`, permissions with `OrgRoles`, `org_id` in every query, the foreign key to `orgs` when it exists, the `org_isolation` policy when the app has a `*_row_level_security.sql` migration, and HTTP tests with real accounts); `--diff` prints the plan as a diff. In an app on `gorbital.Main`, `orb gen resource` runs it, and `orb gen resource --scope org` runs `--org`.
  - `orb gen middleware <Name>` with `--module`, `--module --guard` or `--global`: middleware, a `guard.New` guard with its error, or app-wide middleware, each with a table-driven test. It prints the line that wires it and edits nothing.
  - `orb gen modules` writes `internal/modules/modules.gen.go`, the list of the app's modules for `gorbital.Main` (with `--dry-run` and `--json`); `orb dev` rewrites it before each build when the app has one, and runs `./cmd/api migrate`, `migrate-down` and `seed` in apps without `cmd/migrate`, whose status `orb doctor` also reads.
  - `orb routes` (text and `--json`): every route's method, path, operation ID, module, guards (`x-gorbital-guards`), middleware, handler, source file and line, and public flag; `--module`, `--public`, `--openapi`.
  - `orb doctor` checks `modules.gen.go` against the module directories, custom `gorbital.WithStack` without `Recover` or `Auth`, `APP_REQUEST_TIMEOUT`, and pending migrations of the merged history; its JSON gains `layout`.
  - `orb gen migration`, `orb gen module`, `orb add rls` and `orb add orgs` in an app on `gorbital.Main` give new migrations versions after the newest built-in migration, which the app's `db/migrations` doesn't hold; a version before it was refused on a migrated database.

### New apps, ejecting and upgrading

#### Added

- New Full apps on the new layout ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#phase-9-implementation-notes-new-apps-on-the-v02-layout-2026-09-17)): `orb new --preset full` writes an app on `gorbital.Main`, generated from the ported golden apps `examples/full-single` and `examples/full-multi`: `cmd/api/main.go` with `authhttp`, `opshttp`, `flagshttp`, `mailevents` (and `orgshttp`), the email provider and file storage in `cmd/api`, the `projects` module as `orb gen module` writes it and a `ping` module with a runtime setting and a feature flag, tests through `gorbitaltest`, and `api/surface.json` recording only the app's own names. v0.1 apps keep their layout: the v0.1-layout golden apps move to `examples/v0.1`, `gorbital.lock` records `layout`, and `orb upgrade`, `orb add orgs` and `orb add mail` use the templates of the app's own layout (`orb upgrade --json` and `orb add orgs --json` gain `layout`). In the new layout `orb add mail` replaces `cmd/api/mail.go`, and `orb add storage`, `orb add rls` and `orb add orgs` work. [Getting started](docs/start/quickstart.md) rewritten for the new layout.
- `orb eject <auth|flags|mailevents|ops|orgs>` ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#phase-9-implementation-notes-orb-eject-2026-09-17), guide: [The code in your repo](docs/guides/the-code-in-your-repo.md)): copies a built-in module, at the `gorbital.dev/gorbital` version the app builds with, into `internal/modules/<module>` as layered code the app owns, with its tests; changes the app's imports of the package (its name, options and hooks kept), copies its migrations into `db/migrations` under the same versions, records the ejection in `gorbital.lock` (field `ejected`) and runs `go mod tidy`; `--dry-run`, `--diff`, `--json`, `--allow-dirty`, `--skip-tidy`. The API, database and behaviour don't change. `orb gen modules` doesn't list ejected modules; `orb doctor` reports each (`ejected`) and warns when the library's package changed since, quoting the changelog; `orb upgrade` keeps them. `orb eject` records `api/surface.json` again after copying a module: its error codes and audit actions are the app's own names from then on, which the app's `TestPublicSurface` compares.
- `orb upgrade --layout v0.2` ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#phase-9-implementation-notes-orb-upgrade---layout-v02-2026-09-17), docs: [Upgrading apps](docs/start/upgrading.md#move-to-the-v02-layout), recipe [Upgrading a v0.1 app](docs/examples/recipes/upgrading-a-v0.1-app.md)): the opt-in move of a v0.1 app to the new layout. Generated code the app never changed is deleted and the library runs it; a built-in module it changed is kept as the app's own code, copied like `orb eject` with the change placed in the library's file and recorded in `gorbital.lock`; the app's own modules stay in `internal/modules`, their `huma.Register` calls becoming `gorbital` routes (or `operation.Register`) and their error mappings and permissions moving into each `Module` value; middleware added to `internal/app/routes.go` becomes `gorbital.WithStack` in `main.go`, with its files moved to `cmd/api`. `db/migrations` is untouched, so a database the v0.1 app migrated has nothing to apply, and the OpenAPI document only gains `x-gorbital-guards`. The app gets `UPGRADE-v0.2.md`; changes orb can't make stop the move unless `--allow-manual` keeps them under `_upgrade-v0.1/`. Flags: `--dry-run`, `--diff`, `--json`, `--allow-dirty`, `--allow-manual`, `--yes`, `--no-input`, `--skip-tidy`, `--skip-build`.

#### Changed

- The generated architecture test (`internal/modules/architecture_test.go`) lets a module import another module's root package, such as an ejected sign-in's authenticator, and still refuses importing another module's layers.
- Inside `authhttp` and `orgshttp`, every internal package is under a layer (`internal/delivery/jobs/…`, `internal/delivery/signintest`, `internal/repository/migrations`), and the library's own tests are marked `//orb:noeject`; the HTTP tests of `opshttp`, `flagshttp` and `mailevents` carry their own test app. This is what makes each module ejectable.

### The Dev Portal

#### Added

- A tunnel from the Dev Portal ([ADR-0086](docs/adr/0086-dev-portal-tunnel.md), guide: [Tunnel](docs/dev-portal/tunnel.md)): `orb dev --tunnel quick|named` (`--tunnel-hostname`) and the portal's Tunnel screen put the app, and only the app, on a public HTTPS address with the developer's own `cloudflared` (never downloaded; install steps per OS when it's missing). Quick tunnels get a random `trycloudflare.com` URL for webhooks and phones; named tunnels use `CLOUDFLARE_TUNNEL_TOKEN` (or `_FILE`), passed to cloudflared in its environment only and redacted everywhere, and `ORB_TUNNEL_HOSTNAME` or a hostname saved from the portal, for sign-in callbacks and passkeys. Development only; `orb dev` owns cloudflared's process group, streams `tunnel` events, checks `/livez` through the public URL, restarts a quick tunnel when the app's port changes, proposes the `.env` changes (`APP_PUBLIC_URL`, `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS`, `AUTH_DEFAULT_RETURN_TO`, `APP_CORS_ORIGINS`, `APP_TRUSTED_PROXIES`) applied through the env editor, and lists the Google, Apple, GitHub and Resend URLs to register from the app's routes. Endpoints under `/_portal/api/tunnel`. Auth providers guide: [Testing on a real domain](docs/guides/auth-providers.md#testing-on-a-real-domain).
- Testing sign-in from the Dev Portal ([ADR-0087](docs/adr/0087-testing-sign-in-from-the-dev-portal.md), guide: [Testing sign-in](docs/dev-portal/testing-sign-in.md)): the Authentication screen's **Test sign-in** tab, linked from `/v1/auth/*` routes, checks each method of an app on `authhttp` in development without creating accounts, sessions, identity links, audit events or rows. Offline checks with stable codes, fixes and links (client ID and secret shapes, the Apple `.p8` key signing a client secret, `APP_PUBLIC_URL` and its port, passkey RP ID and origins, `AUTH_ENCRYPTION_KEYS`, `MAIL_DELIVERY`); network checks for Google, Apple and GitHub (reachability, clock skew, client credentials); **Test now** round trips through the real provider to the app's real callback, which recognises the test's in-memory, single-use state before sign-in and verifies the code and identity as sign-in does, reporting `redirect_uri_mismatch`, `invalid_client`, `audience_mismatch`, `clock_skew` and others; ID tokens from mobile SDKs; a passkey ceremony on `APP_PUBLIC_URL` (`/_signin-test/passkey`, with a single-use ticket); a throwaway authenticator app secret with clock drift reported; and the test email. Endpoints under `/_dev/auth/test/`. `devconsole.Sources.Extensions` (`devconsole.Extension`, `Index.Extensions`) let built-in modules serve endpoints behind the dev console's checks. Auth providers guide: [Test your configuration](docs/guides/auth-providers.md#test-your-configuration).
- The Routes screen shows guards, public routes, middleware and sources (`GET /_portal/api/routes`), also while the app is stopped; the generators hub has Module and Middleware forms, the module form offering `--org`; `GET /_portal/api/jobs` lists the jobs a Main app's modules declare in `internal/modules`, where there is no `internal/app` (the Jobs screen's runtime list comes from `/ops/jobs` as before).

#### Fixed

- The Schema screen no longer crashes after the Table Editor (shared query keys).

### Documentation, examples and quality gates

#### Added

- Versioned documentation ([ADR-0084](docs/adr/0084-versioned-documentation.md)): v0.1 stays published unchanged, the v0.2 line is previewed under `/next/`, and the site gains Methods and Examples tabs. Methods pages are generated from doc comments and `Example` functions and checked in CI.
- Shelfie, the example app of the Examples tab (`examples/apps/shelfie`), written chapter by chapter in twelve chapters from [0. Start a project](docs/examples/shelfie/00-start-a-project.md) to [11. Deploy](docs/examples/shelfie/11-deploy.md): a books module, protecting routes, guards and middleware of its own, tests, operations, accounts, a phone-code sign-in method, book clubs, the generators, hardening and a deployment. Its modules use four layers with one file per operation ([ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md#3-app-layout)).
- Five recipes, each with its own app in `examples/apps`: [Multi-tenant invoicing](docs/examples/recipes/multi-tenant-invoicing.md) (companies as organisations, an invoices module from `orb gen module --org`, row-level security tested as a database role without `BYPASSRLS`), [Internal admin tool](docs/examples/recipes/internal-admin-tool.md) (sign-in without sign-up, a role requiring a second factor, the audit log as a workflow), [Receiving payment webhooks](docs/examples/recipes/receiving-payment-webhooks.md), [Mobile backend with an external identity provider](docs/examples/recipes/mobile-backend-with-an-idp.md) and [Upgrading a v0.1 app](docs/examples/recipes/upgrading-a-v0.1-app.md). Every code block on a page is included from one of the apps, and all five are built and tested in CI.
- Quality gates: fuzz tests for the parsers that take untrusted input (`scripts/fuzz.sh`, `fuzz.yml`), benchmark comparisons on pull requests (`bench.yml`) with measured baselines and budgets in [benchmarks](docs/benchmarks.md), apps generated by `orb v0.1.0` built and tested against every change, and frozen v0.1.0 contracts checked by `internal/tools/contracts`.
- [Benchmarks](docs/benchmarks.md) measured again for the release: the route adapter and each guard stay within their budgets, and the golden apps' dependency counts, binary sizes, start-up times and memory are recorded next to the v0.1.0 baselines.
- `internal/tools/docscheck`: a check that every Markdown link, `#anchor` and `<!-- include -->` marker in `docs/` resolves and that `docs/docs.json` lists each page once, run in CI. Until now a broken include failed only in the docs site's build.
- An internal security review of the v0.2 diff — the sign-in extraction, guards, hooks, JWT and webhooks — in [docs/security](docs/security/README.md).

#### Changed

- Architecture principle 3 now reads "library for behaviour and default wiring; generation for scaffolding and the module list" ([architecture](docs/architecture.md#2-principles)).

## v0.1.0 (2026-09-17)

The first public release. It contains everything built during development, grouped below by theme: the foundation, data and identity, strong authentication, organisations, operations and upgrades, the stability and security review work, operations and integrations, and the Dev Portal. `v1.0.0` follows once the external security review signs off ([stability](docs/guides/stability.md)).

Numbers such as v0.2 or v1.1 in older decision records name internal development milestones, not releases. *Changed*, *Fixed* and *Security* entries describe changes from earlier development builds; apps created with one follow the [upgrade notes](docs/guides/upgrade-notes.md#before-v010-development-builds).

### Foundation

#### Added

- Core packages, OpenAPI with Huma, OpenTelemetry, the Minimal preset, `orb new`, `orb dev`, `orb version`, project CI and the signed release workflow.

### Data and identity

#### Added

- The Full preset: PostgreSQL, runtime settings, background jobs on River, email through Resend or SMTP, audit log, email and password authentication, platform roles and permissions, ops APIs, seed data.
- `orb dev` with Docker services, `orb gen resource`, `orb gen job`, `orb gen migration`, `orb add mail`.

### Strong authentication

#### Added

- Two-factor authentication with authenticator apps and recovery codes; 2FA required for ops roles ([ADR-0043](docs/adr/0043-two-factor-authentication.md)).
- Passkeys, for passwordless sign-in and as a second factor ([ADR-0044](docs/adr/0044-passkeys.md)).
- Google and Apple sign-in, web and native ([ADR-0046](docs/adr/0046-google-and-apple-sign-in.md)); sign-in provider setup and `AUTH_PROVIDERS.md` ([ADR-0045](docs/adr/0045-sign-in-provider-setup.md)).

### Organisations

#### Added

- Multi-tenant organisations: `modules/orgs`, `orb new --tenancy multi`, personal workspaces, memberships and org roles, invitations, soft delete, restore and purge ([ADR-0048](docs/adr/0048-organisations-v0-4.md)).
- `orb gen resource --scope org` with generated cross-organisation denial tests.

### Operations and upgrades

#### Added

- `orb upgrade`: 3-way merges of template changes on a branch, with the merge base rebuilt from the recorded release and proven against `gorbital.lock` v2 ([ADR-0050](docs/adr/0050-upgrades-and-adding-features.md)).
- `orb add orgs`: turns a single-tenant Full app multi-tenant, with migrations that move existing data.
- `orb doctor`.
- Ops endpoints: `GET /ops/system`, `/ops/audit/stats`, `/ops/jobs/overview`, `/ops/retention`; retention settings and the daily `retention` job; maintenance mode ([ADR-0051](docs/adr/0051-operations-v0-5.md)).
- `api/postman_collection.json` and `api/llms.txt`, generated with the OpenAPI document.

#### Changed

- Audit events older than 365 days are deleted by default.

### Stability and security review

#### Added

- Rate limits shared by every instance, counted in PostgreSQL by `modules/ratelimitpg`, with an in-memory fallback; sign-in limits are runtime settings ([ADR-0052](docs/adr/0052-shared-rate-limits.md)).
- `APP_TRUSTED_PROXIES` and `httpx.TrustedProxies`: client IPs from `X-Forwarded-For` only through configured proxies.
- Apple tokens revoked by the `auth_revoke_tokens` job, with retries.
- Governance, contribution guide, code owners and issue and pull request templates ([ADR-0055](docs/adr/0055-governance-and-contribution.md)).
- API freeze checks ([ADR-0054](docs/adr/0054-api-freeze-and-scaffold-compatibility.md)): `Stability:` markers on every package, API listings in `api/*.txt` checked by `internal/tools/apicheck`, `api/surface.json` and `api/openapi.baseline.json` with their tests in Full apps, `openapi.CheckCompatible`, `gorelease` on library tags, and the scaffold compatibility check.
- `"schemaVersion": 1` in every `orb --json` output; `orb version --json`.
- `settings.(*Registry).Keys` and `jobs.(*Definitions).Names`.
- `POST /v1/auth/identities` to link Google or Apple while signed in; `social.Identity.HostedDomain` and `AuthoritativeEmail`.
- `APP_TRUSTED_CALLERS`, `httpx.RequestIDFrom` and `telemetry.WithTraceContextFrom`: request IDs and trace context kept only from trusted callers.
- Runtime settings `auth.login_address_attempts`, `auth.reauth_attempts`, `auth.code_attempts`, `auth.code_window`, `auth.unverified_account_ttl`, `orgs.max_owned` and `orgs.user_invitations_per_hour`; audit actions `auth.reauth.failed` and `auth.accounts.unverified_expired`; error codes `social_link_required`, `identity_in_use`, `too_many_orgs`, `job_run_limited`, `job_not_retryable` and `audit_query_timeout`.
- `ratelimit.ClientKey`, `actor.WithClient`, `mail.RedactAddresses`, `orgs.Authorize`, `openapi.WithoutSpecEndpoints`, `settings.DefaultMaxItems`, `auditpg.WithQueryTimeout`, `jobs.Manager.PauseQueueWithReason` and `ResumeQueueWithReason`, `auth.NewHasherWith` with context-aware hashing.
- The internal security review report ([docs/security](docs/security/2026-09-internal-review.md), [ADR-0053](docs/adr/0053-internal-security-review.md)); the CLI guide explains how to verify release signatures and provenance.
- Reference pages for error codes, audit actions, permissions and roles, runtime settings and jobs in [docs/reference](docs/reference/error-codes.md), generated from the golden apps by `internal/tools/refdocs` and checked in CI; this changelog on the docs site.

#### Changed

- Generated apps refuse to start without `APP_ENV`; `orb dev` sets `development` (HTTP-6).
- API docs and the OpenAPI document are off by default in production and switched together by `APP_DOCS_ENABLED` (HTTP-3).
- `jobs.Manager.PauseQueue` is deprecated and returns `ErrReasonRequired`; use `PauseQueueWithReason` (OPS-2).
- `ratelimit.ByRemoteIP` groups IPv6 addresses by /64; `httpx.RequestID` ignores incoming `X-Request-ID`; `auth.NewRecoveryCodes` makes 16-character codes.
- HTTP server metrics drop `server.address` and `server.port` (HTTP-2).
- `/readyz` shares its checks among concurrent requests and reuses the result for 1 s (HTTP-7).
- The `orgs_purge` job clears its whole backlog per run (ORG-3).
- `orb version` and `orb doctor` warn when `orb` was built with Go older than 1.26.5; the Minimal preset and `modules/telemetry` use grpc v1.83.2 (CLI-7).
- The `orb` release job needs approval in the `release` environment and builds only commits on `main`, without caches (CLI-6).
- `go generate ./internal/recipes/` needs git: templates come only from files git tracks or would track (CLI-1).

#### Fixed

- `orb`'s template generator no longer copies a golden app's local `.env` file into templates.
- Accepting and resending the same invitation at once can no longer deadlock (ORG-7).
- A deleted account no longer stays an owner of organisations that were deleted at the time (ORG-6).
- The production guide no longer recommends custom forwarded-header middleware or describes per-instance rate limits (HTTP-8).

#### Security

Fixes from the internal security review; details, tests and accepted items in the [report](docs/security/2026-09-internal-review.md).

- A password chosen by whoever registered an address before its owner no longer survives the owner's verification; verifying an address removes sessions, passkeys, authenticator apps, recovery codes and identities added before; roles only for verified accounts; unverified accounts expire after 7 days (AUTH-S-1).
- Verification and reset codes allow 20 checks a day per address across all codes (AUTH-S-2).
- `auth.NormalizeEmail` refuses addresses whose non-ASCII characters change when lowercased (AUTH-S-3).
- Registration, verification resend and forgot password take the same minimum time whether or not the address has an account (AUTH-S-4).
- Password and second-factor checks behind a session are limited per user and audited, so a stolen session isn't a password oracle (AUTH-S-5, AUTH-M-2).
- Sign-in limits count per address and client network, with a looser per-address limit, so others can't lock an account out (AUTH-S-6).
- Per-IP limits group IPv6 clients by /64 (AUTH-S-7, OPS-1).
- Password hashing waits a bounded time for a slot; password reset checks the code before hashing (AUTH-S-8).
- Google and Apple sign-in link existing accounts, and mark new ones verified, only for addresses the provider is authoritative for (AUTH-M-1).
- Apple server-to-server notifications are refused after an hour and processed once (AUTH-M-3).
- Recovery codes carry 80 random bits instead of 50 (AUTH-M-4).
- Organisation invitations can't be accepted after the inviter is removed or demoted (ORG-1).
- Organisation role assignment compares permissions, so admins can't grant custom roles more powerful than their own (ORG-2).
- Creating organisations and inviting need a verified address and are limited per user (ORG-3).
- Organisation use cases re-check the caller's role under the organisation lock (ORG-4); restoring an organisation applies the role's two-factor requirement (ORG-5).
- Job timeout, attempt and queue changes and queue pauses require a reason; run-now is limited to one queued run and one a minute (OPS-2).
- Retrying a completed run, or a run of a disabled job, is refused (OPS-3).
- Audit metadata redaction matches plural and camelCase keys and more sensitive names (OPS-4).
- Email addresses in mail provider replies no longer appear in job errors or logs (OPS-5).
- `GET /ops/audit` and audit stats queries stop after 5 seconds (OPS-6).
- Every audit event recorded during a request carries the client IP and user agent (OPS-7).
- Test emails are authorised before queueing and limited to 5 an hour per operator (OPS-8).
- Sender, invitation and verification-code settings require a reason (OPS-9); `StringList` settings are capped at 100 items by default (OPS-10).
- Clients can no longer control tracing: incoming trace context starts a new linked trace unless the caller is trusted, baggage is ignored, and spans' `client.address` no longer comes from `X-Forwarded-For` (HTTP-1).
- Clients can't exhaust metric cardinality with `Host` headers (HTTP-2).
- `APP_DOCS_ENABLED=false` also stops serving the OpenAPI document (HTTP-3).
- Apps refuse `http://` CORS origins in production; `httpx.CORS` refuses origins with user info, paths or queries (HTTP-4).
- Clients can't reuse other requests' IDs in logs, audit events and jobs (HTTP-5).
- `/readyz` floods can't exhaust the database pool (HTTP-7).
- Templates are generated only from git's file set, so git-ignored local files (keys, `.env`) never reach `orb` or new apps (CLI-1).
- `orb add mail` makes an existing `.env` private (0600) (CLI-2).
- `orb upgrade` recognises `GOPRIVATE`, `GONOSUMDB` and `GOINSECURE` patterns with a trailing slash and refuses checksum databases other than sum.golang.org (CLI-3).
- `orb` validates the app name, module path, preset, tenancy and mail provider read from `gorbital.lock` and `gorbital.yaml` before rendering templates (CLI-4).
- `orb upgrade` reads earlier releases only from a gorbital checkout outside the app's repository (CLI-5).

### Operations and integrations

#### Added

- Per-organisation settings ([ADR-0056](docs/adr/0056-per-organisation-settings.md)): `settings.OrgOverridable()`; organisation values returned by `Setting.Get` from the context; `Store.ListForOrg`, `GetForOrg`, `SetForOrg`, `ResetForOrg`, `HistoryForOrg`, `Overrides`; `settings.ErrNotOrgOverridable`, `settings.ErrInvalidOrgID`; `View.OrgOverridable`, `View.OrgID`, `View.PlatformValue`, `HistoryEntry.OrgID`.
- Multi-tenant Full apps: `GET /v1/orgs/{orgId}/settings`, `GET|PUT|DELETE /v1/orgs/{orgId}/settings/{key}`, `GET /v1/orgs/{orgId}/settings/{key}/history`; organisation permissions `orgs.settings.read` and `orgs.settings.write`; `orgs.invitation_ttl` can be set per organisation. All Full apps: `GET /ops/settings/{key}/overrides` and `org_overridable` in setting responses.
- `modules/idempotency` ([ADR-0060](docs/adr/0060-idempotency-keys.md), [guide](docs/guides/idempotency.md)): `Idempotency-Key` on POST and PATCH requests, stored per caller in PostgreSQL and replayed with `Idempotent-Replayed: true`; 422 `idempotency_key_reused` for a different request, 409 `idempotency_in_progress` while running, 400 `invalid_idempotency_key`; server errors, responses marked `Cache-Control: no-store` and `idempotency.DontStore` release the key.
- Full apps: the idempotency middleware after authentication (skipping `/v1/auth/*`), the header documented on POST and PATCH operations, setting `idempotency.retention`, job `idempotency_cleanup`, `idempotency_keys` in `/ops/retention`; CORS allows `Idempotency-Key` and exposes `Idempotent-Replayed`.
- Email suppression list and Resend webhooks ([ADR-0062](docs/adr/0062-resend-webhooks-and-suppression-list.md)): `mail.SuppressionList`, `WithSuppressionList`, `ErrSuppressed` (wraps `ErrRejected`) and `NormalizeAddress`; `resend.VerifyWebhook`, `CheckWebhookSecret`, `ParseWebhookEvent`, `ErrInvalidWebhook`, `ErrWebhookTimestamp`, `WebhookTolerance`; new `modules/mail/suppressionpg`.
- Full apps: `POST /v1/webhooks/resend` (with `RESEND_WEBHOOK_SECRET`), the mail worker skipping suppressed addresses, `GET /ops/mail/suppressions`, `DELETE /ops/mail/suppressions/{id}`, permission `ops.mail.write`, audit actions `mail.suppression.added` and `mail.suppression.removed`; `GET /ops/mail` reports `webhook_secret` for Resend; `orb add mail --provider resend` adds `RESEND_WEBHOOK_SECRET` to `.env.example` and a next step for the webhook.
- Prometheus metrics ([ADR-0063](docs/adr/0063-prometheus-metrics.md)): `METRICS_ADDR` serves `GET /metrics` on a separate listener, off by default, in every preset; `telemetry.WithPrometheus`, `(*Telemetry).MetricsHandler`.
- Go runtime metrics (`telemetry.WithRuntimeMetrics`) and PostgreSQL pool metrics (`postgres.WithMeterProvider`), exported over OTLP and Prometheus; `telemetry.RecordRoute` gives spans and HTTP metrics the matched route pattern.
- `modules/flags` ([ADR-0057](docs/adr/0057-feature-flags.md), [guide](docs/guides/feature-flags.md)): feature flags declared in code with organisation and user allow/deny lists, stable percentage rollouts (SHA-256 buckets), PostgreSQL storage with versions, history and required reasons, `LISTEN/NOTIFY` reload, audit events `flags.flag.changed` and `flags.flag.reset`.
- Full apps: `GET /ops/flags`, `GET|PUT|DELETE /ops/flags/{key}`, `GET /ops/flags/{key}/history` (permissions `ops.flags.read`, `ops.flags.write`); `GET /v1/flags` for signed-in clients (permission `flags.flag.read`, given by the `user` role); `GET /v1/orgs/{orgId}/flags` in multi-tenant apps; example flag `example.ping_time` adding an optional `server_time` to `GET /v1/ping`; migration `20260918000010_flags.sql`; `flags_history` in `/ops/retention`; `api/surface.json` gains a `flags` list.
- API keys and service accounts ([ADR-0058](docs/adr/0058-api-keys-and-service-accounts.md), [guide](docs/guides/api-keys.md)): `gorbital.dev/modules/auth` gains `NewAPIKey`, `ParseAPIKey`, `IsAPIKey`, `HashAPIKey`, `APIKeyMatches`, `APIKeyAuthenticator`, `WithAPIKeys`, `RateLimitError`/`ErrRateLimited`, `Principal.APIKeyID`/`Scopes`/`ServiceAccountID`/`OrgID`, `Principal.APIKey()`, `Principal.Restrict`, `DefaultAPIKeyMaxTTL`, `APIKeyTTLLimits`, `APIKeyTouchInterval`, `APIKeyRetention`.
- Full apps: `GET|POST /v1/auth/api-keys`, `DELETE /v1/auth/api-keys/{id}`; `/ops/service-accounts` (list, create, get, update, delete, keys list, create, revoke); multi-tenant apps: the same under `/v1/orgs/{orgId}/service-accounts`. Settings `auth.api_key_max_ttl`, `auth.api_key_failures_per_minute`; permissions `ops.service_accounts.read`, `ops.service_accounts.write`, `orgs.service_accounts.manage`; audit actions `auth.api_key.created|revoked|expired`, `auth.service_account.created|updated|disabled|deleted`; migration `20260918000020_auth_api_keys.sql`.
- Full apps: a `user` platform role every user holds, granting what any signed-in user may do, so API key scopes limit every operation: `flags.flag.read`; single-tenant `projects.project.read|write`; multi-tenant `orgs.org.create`, `orgs.org.list` ([ADR-0058](docs/adr/0058-api-keys-and-service-accounts.md#scopes-cover-every-operation-2026-09-16)).
- GitHub sign-in ([ADR-0059](docs/adr/0059-github-sign-in.md), [guide](docs/sign-in/github.md)): `modules/auth/social` gains `GitHub`, `NewGitHub`, `GitHubConfig`, `GitHubEndpoints`, `Endpoints.APIURL` and `ErrNotSupported` (PKCE, the verified primary email from GitHub's API, the numeric user ID as subject); `socialtest` gains a fake GitHub (`GitHubEndpoints`, `GitHubCode`, `GitHubUser`, `GitHubEmail`) checking PKCE, the bearer token and API headers.
- Full apps: `GET /v1/auth/github/start`, `GET /v1/auth/github/callback`, `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`; linking GitHub while signed in (`POST /v1/auth/{provider}/link`, `github` only); `github` in the sign-in methods status; migration `20260918000030_auth_github.sql`; `flow` metadata on `auth.identity.linked`.
- `modules/observability` ([ADR-0064](docs/adr/0064-live-observability-and-incidents.md), [guide](docs/guides/observability.md)): per-instance request collector (`Collector`, `Middleware`, `RecordRoute`, `Subscribe`) writing per-minute counts and latency histograms to PostgreSQL, `Store.Summary` across instances with `Stats.Quantile`, incidents with timelines (`OpenIncident`, `UpdateIncident`, `ResolveIncident`, `Incidents`), `DetectIncident` (one open automatic incident across instances) and `Streams` limits for long-lived responses.
- Full apps: `GET /ops/observability/overview`, `/ops/observability/routes`, `/ops/observability/stream` (Server-Sent Events); `POST|GET /ops/incidents`, `GET /ops/incidents/{id}`, `POST /ops/incidents/{id}/updates`, `POST /ops/incidents/{id}/resolve`, `GET /ops/incidents/{id}/report` (JSON or Markdown); permissions `ops.observability.read`, `ops.incidents.read`, `ops.incidents.write`; settings `observability.retention`, `incidents.detection_window`, `incidents.error_rate_threshold`, `incidents.min_requests`; jobs `observability_cleanup`, `incidents_detect`; audit actions `ops.incident.opened`, `ops.incident.updated`, `ops.incident.resolved`; migrations `20260918000060_observability_minutes.sql`, `20260918000061_incidents.sql`.
- `modules/postgres` row-level security support ([ADR-0061](docs/adr/0061-row-level-security.md), [guide](docs/guides/row-level-security.md)): `OrgSetting`, `BypassSetting`, `WithOrg`, `WithoutRowLevelSecurity`, `WithLogger`, `CheckRowLevelSecurity`, `RowLevelSecurityReport`; every pool sets the organisation on its connections; `Migrate` bypasses policies.
- `orb add rls`: row-level security for multi-tenant apps; `orb gen resource --scope org` adds the policy in apps that ran it; `--json` of `orb gen resource` gains `row_level_security`; `orb doctor` gains a `row-level security` check.
- Full apps warn at startup, and `go run ./cmd/migrate --status` reports (`row_level_security`), when row-level security is on and the database role bypasses it, a table isn't forced, or an organisation table has no policy.
- `modules/devconsole` ([ADR-0065](docs/adr/0065-local-dev-console-apis.md), [guide](docs/guides/dev-console.md)): development-only `/_dev/` APIs (app wiring, routes, configuration without secrets, recent requests and logs with streams, Mailpit email, migrations, job runs) behind Host, loopback and bearer token checks, with its own OpenAPI document.
- `telemetry.WithLogTee` sends log records to a second handler with its own level.
- All golden apps serve the dev console APIs in development when `DEV_CONSOLE_TOKEN` is set; production refuses the variable.
- `orb dev` generates a dev console token per run, passes it to the app without writing it to disk, and prints the `/_dev/` address and token.

#### Changed

- `orb add orgs` also copies organisation-only migrations added after organisations shipped (`recipes.OrgsLaterMigrationPaths`).
- `modules/telemetry` depends on the OpenTelemetry Prometheus exporter and runtime instrumentation (and `prometheus/client_golang`).
- `gorbital.dev/modules/orgs`: `RequireMember` and `Authorize` also accept an organisation service account's API key (only in its own organisation) and limit every API key's organisation permissions to its scopes, never step-up permissions.
- `auth.Middleware` never passes a `gbk_` token to the session authenticator; `auth.NewToken` never returns one; `auth.WithPrincipal` applies `Principal.Restrict` (no effect on sessions).
- `modules/idempotency` never stores responses marked `Cache-Control: no-store`, so a retried API key creation makes a new key instead of replaying the first.
- Full apps: account-management use cases (`/v1/auth/me`, sessions, password, 2FA, passkeys, identities, deletion) answer 403 `session_required` to API keys; password reset and claiming an address revoke the account's API keys; the organisation purge deletes the organisation's service accounts; in multi-tenant apps, accepting an invitation and leaving an organisation answer 403 `session_required` to API keys.
- Full apps: session permission lists (`/v1/auth/me`) include the `user` role's permissions.
- `orb gen resource --scope user`: generated use cases check `<module>.<resource>.read|write`, granted by the `user` role through a line after `//orb:anchor user-permissions` in `internal/app/permissions.go`; `orb doctor` checks that anchor.
- Full apps: `AUTH_DEFAULT_RETURN_TO` sets where browser sign-ins without `return_to` end; required in production with Google, Apple web or GitHub sign-in (and in development with `APP_DOCS_ENABLED=false`), validated against `APP_PUBLIC_URL` and `APP_CORS_ORIGINS`.
- Full apps: the observability collector middleware runs after tracing, and `routes.go` wraps the mux as `telemetry.RecordRoute(observability.RecordRoute(mux))`.
- Multi-tenant apps' `internal/app` tests connect the app as the `gorbital_app_test` role (no superuser, no `BYPASSRLS`), created and granted by the tests.
- Full apps read `MAILPIT_WEB_PORT` (before, only `compose.yaml` and `orb dev` did) for the dev console's `/_dev/mail`.

#### Fixed

- Full apps' HTTP server spans were named after the method only (`GET`) and HTTP metrics had no `http.route`, because the session middleware passes a copy of the request to the mux; `routes.go` now wraps the mux with `telemetry.RecordRoute`.
- Full apps: Google and Apple web sign-ins without `return_to` redirected to `APP_PUBLIC_URL/docs`, a 404 in production since docs are off there (security review follow-up to HTTP-3).
- Full apps: an Apple configuration for iOS apps only (`APPLE_BUNDLE_IDS` without `APPLE_SERVICES_ID`) no longer needs `APP_PUBLIC_URL` when the app is built.

### Dev Portal ([ADR-0066](docs/adr/0066-dev-portal.md), [roadmap](docs/dev-portal-roadmap.md))

#### Added

- `orb dev` serves the Dev Portal at http://127.0.0.1:3100 and opens it in the browser: the portal UI (a static export of gorbital-dashboards' `apps/devtools`, embedded in `orb` by `scripts/sync-portal.sh`; a plain checkout serves a placeholder page), `GET /_portal/api/status`, `GET /_portal/api/output`, `GET /_portal/api/events` (Server-Sent Events with the app's state and output), `POST /_portal/api/app/restart`, `stop` and `start`, `POST /_portal/api/generators/{job|resource|migration}/plan` and `/apply`, and a proxy under `/_portal/app/` that adds the dev console token to `/_dev/` requests. A per-run token in the printed link sets an `HttpOnly`, `SameSite=Strict` cookie; every API request needs a loopback `Host`, a loopback peer, the token and, for writes, an `X-Orb-Portal` header. Flags `--portal-port`, `--no-portal`, `--no-open`; `DEV_PORTAL_PORT` in `.env`; `DEV_PORTAL_TOKEN` in `orb dev`'s environment ([guide](docs/guides/dev-portal.md)).
- `orb dev` is a supervisor: it keeps the app's state (preparing, building, running, stopped, with the last build or migration problem), copies the app's and its own output for the portal, and takes restart, stop and start requests.
- The Dev Portal has a light theme next to the dark one: it follows the system's colour scheme the first time (dark by default), the sun or moon button in the top bar and "Toggle theme" in the ⌘K palette switch it, and the choice is remembered per browser. Every screen, the charts, the SQL editor and the schema diagram render in both. The Table Editor's list keeps long table names inside the sidebar, and the New table sheet is wide enough for a row of controls per column ([guide](docs/guides/dev-portal.md#light-and-dark-theme)).
- Dev Portal phase 12 ([ADR-0077](docs/adr/0077-generators-hub-first-run-and-project-settings.md)): the portal's generators `add-mail`, `add-storage`, `add-rls` (plans with a diff, applied as the commands write) and `add-orgs` (the command's dry run, then its branch workflow); `orb new` runs `orb dev` and opens the Dev Portal when created in a terminal (`--no-start` keeps the old ending, `--start` forces it); `GET /_portal/api/project` (the app's settings with the environment key behind each, never secrets) and `POST /_portal/api/project/reset-database` (drops the schema, then the supervisor applies migrations and seed data); the portal's generators hub and Project Settings screens.
- Dev Portal phase 11 ([ADR-0076](docs/adr/0076-git-screen.md)): `orb dev` runs the developer's `git` for the portal at `/_portal/api/git/…`: `status`, `diff`, `stage`, `unstage`, `patch` (hunks), `discard`, `commit`, `branches` (list, create, delete with `force` after `unmerged`), `switch`, `fetch`, `pull`, `push` (never force), `merge/preview` (`git merge-tree`), `merge`, `merge/abort`, `conflicts`, `log` (parents and refs), `open` (the developer's editor: `ORB_EDITOR`, `VISUAL`, `code --goto`, the system opener); git's refusals answer 409 `git_refused`. The portal's Git screen.
- Dev Portal phase 10 ([ADR-0075](docs/adr/0075-file-storage.md)): `gorbital.dev/modules/storage`, a `Store` of objects with `local` (files under a directory, HMAC-signed links served by the app at `/storage/`) and `s3` (Amazon S3, DigitalOcean Spaces, Cloudflare R2, MinIO; presigned URLs) drivers; Full apps wire it from `STORAGE_DRIVER`, `STORAGE_LOCAL_DIR`, `STORAGE_ENDPOINT`, `STORAGE_REGION`, `STORAGE_BUCKET`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY`, `STORAGE_PUBLIC_URL`, `STORAGE_PATH_STYLE`, `STORAGE_SIGNING_KEY` (`local` by default in development, refused in production) and serve `GET /ops/storage`, `GET /ops/storage/objects`, `GET|PUT|DELETE /ops/storage/object`, `GET /ops/storage/object/content`, `POST /ops/storage/object/move`, `POST /ops/storage/directories`, `POST /ops/storage/signed-url` behind `ops.storage.read` (`ops_viewer`) and `ops.storage.write` (`platform_admin`), audited; error codes `storage_off`, `storage_object_not_found`, `invalid_storage_key`, `storage_unavailable`; `orb add storage --driver local|s3|spaces|r2|minio` (MinIO joins `compose.yaml` and `orb dev`). The portal's Storage screen.
- Dev Portal phase 9 ([ADR-0074](docs/adr/0074-dev-mail-previews-and-env-editor.md)): `orb dev` catches the app's email (`MAIL_DELIVERY=devmail`, now the development default; `DEV_MAIL_SMTP_ADDR`, default `127.0.0.1:1025`) into `.orb/portal/mail` and serves the inbox at `GET /_portal/api/mail` (search, paging), `mail/stream`, `mail/{id}` (text, HTML, source, headers, links, verification codes, attachments), `mail/{id}/html` (sandboxed), `mail/{id}/source`, `DELETE mail/{id}` and `DELETE mail`; Mailpit is gone from the Full compose template (`mailpit` delivery still works for apps that keep it); `orb dev` waits only for the services `compose.yaml` defines. Email previews: `auth.EmailPreviews(appName)`, the golden apps' `mailPreviews()`, and the dev console's `GET /_dev/mail/previews`, `GET /_dev/mail/preview?name=&to=` and `POST /_dev/mail/preview/send?name=&to=` (`devconsole.MailPreviewer`; console endpoints can be POST). The `.env` editor: `GET /_portal/api/env` (every key of `.env` and `.env.example`, secrets masked, missing keys flagged, the example's comments as descriptions), `GET env/{key}` (reveal), `PUT env` (set and unset in place). The portal's Mail and Environment screens.
- Dev Portal phase 8 ([ADR-0073](docs/adr/0073-observability-screen.md)): `orb dev` serves `GET /_portal/api/health` (app, PostgreSQL, Mailpit, Compose services), `GET /_portal/api/system` (the machine, the app process and `orb`, sampled every 2 s with `gopsutil`), `GET /_portal/api/db/stats` (connections by application and state against `max_connections`, cache and index hit ratios, transactions, the largest tables with scans and dead rows, lock waits, long-running statements), `GET /_portal/api/db/statements` and `POST …/reset` (`pg_stat_statements`; the development `compose.yaml` preloads it and the extension is created on first use), `GET /_portal/api/db/advice` (foreign keys without an index, unused indexes, sequentially scanned tables, tables waiting for a vacuum, each with the SQL). The operators' account APIs answer 409 `email_taken` and 422 `unknown_role` instead of 500. The portal's Observability screen.
- Dev Portal phase 7 ([ADR-0072](docs/adr/0072-local-log-store.md)): `orb dev` keeps the app's log records under `.orb/portal/logs` (JSON Lines segments, 8 MiB each, 64 MiB in all) from the app's output, its own messages and, with services, the PostgreSQL container; `GET /_portal/api/logs` with filters (time, level, source, user, method, path, status class, duration, request and trace ID, text) and paging, `logs/histogram`, `logs/stream` (live tail), `logs/errors` (groups by fingerprint), `logs/request/{id}`, `logs/stats`, `DELETE logs`, saved filters under `logs/filters`; `orb dev`'s command output (migrations, seeds, Docker) reaches the portal; the next migration version skips versions goose already applied. `httpx.AccessLog` records `source=http`, `path` and the attributes later middleware add through `httpx.AccessNote` (`auth.Middleware` adds `user_id`). Apps: `APP_LOG_FORMAT` (`json` or `text`; empty keeps JSON in production and text elsewhere; `orb dev` sets `json` and shows text), and the jobs, auth and mail loggers carry `source`. The portal's Logs screen.
- Dev Portal phase 6 ([ADR-0071](docs/adr/0071-job-kinds-and-ejection.md)): `orb gen job --kind custom|http|sql|email|dispatch` with `--method`, `--url`, `--body`, `--sql`, `--to`, `--subject`, `--text`, `--dispatch`; each kind is rendered as its own `Work` with a test; the definition file carries an `//orb:job {json}` marker (the kind, its fields and the worker file's SHA-256) that the portal reads back, and a worker edited by hand is ejected; Full apps' `jobDeps` gain `pool`, `mailer`, `httpClient` and `runJob`; `GET /_portal/api/jobs` lists the app's jobs from code with `generated`, `ejected`, `kind` and `form`. The portal's Jobs screen makes jobs by form, CLI or code.
- Dev Portal phase 5 ([ADR-0070](docs/adr/0070-operators-account-apis.md)): operators' account APIs in the auth module, `GET|POST /ops/auth/users`, `GET|DELETE /ops/auth/users/{id}`, `POST …/verify-email`, `…/ban`, `…/unban`, `…/roles`, `DELETE …/roles/{role}`, `DELETE …/sessions[/{sessionId}]`, `DELETE …/passkeys/{passkeyId}`, `DELETE …/identities/{identityId}`, `POST …/mfa/enroll`, `…/mfa/reset`, `…/impersonate` (development only); permission `ops.auth.write` (platform_admin); bans (`auth_users.banned_at`, `banned_reason`; every sign-in answers 403 `account_banned`); audit actions `auth.user.banned`, `auth.user.unbanned`, `auth.user.deleted_by_operator`, `auth.email.verified_by_operator`, `auth.session.revoked_by_operator`, `auth.passkey.removed_by_operator`, `auth.identity.removed_by_operator`, `auth.user.impersonated`, `ops.rate_limit.reset`; error codes `account_banned`, `impersonation_off`, `user_not_found`, `rate_limiter_not_found`; `GET /ops/auth/rate-limits` and `POST /ops/auth/rate-limits/reset` with `ratelimitpg.(*Store).Reset`; migration `20260918000070_auth_bans.sql`. The portal's Authentication screen.
- Dev Portal phase 4 ([ADR-0069](docs/adr/0069-schema-visualiser-objects-and-migrations.md)): `postgres.MigrateDown` and `postgres.MigrationList`; Full apps' `cmd/migrate --down` and `--redo` (development only; `app.MigrateDown` refuses in production); `pgmeta.Migrations` lists the files under `db/migrations` with their state; plan kinds `create_extension`, `drop_extension`, `create_function`, `drop_function`, `create_trigger`, `drop_trigger`, `create_view`, `drop_view`, `drop_enum`; the portal's `GET /_portal/api/db/migrations`, `POST /_portal/api/app/migrate-down` and `migrate-redo`, and its Schema, Objects and Migrations screens.
- Dev Portal phase 3 ([ADR-0068](docs/adr/0068-sql-editor.md)): the SQL editor. `pgmeta.Run` runs a script in one transaction (rolled back by default, committed or read-only on request) through the simple protocol and returns every statement's result as text cells and the server's error with its position; `pgmeta.Explain`, `pgmeta.Check` (warnings for drops, truncates, deletes and updates without WHERE, dropped columns, type changes) and `pgmeta.Templates`; snippets as files under `db/queries`, favourites and history under `.orb/portal`; `orb dev` serves them at `/_portal/api/db/sql/…` (`run`, `explain`, `check`, `templates`, `snippets`, `history`, `migration`).
- Dev Portal phase 2 ([ADR-0067](docs/adr/0067-table-editor-and-pgmeta.md)): `cli/internal/pgmeta` reads the database's catalog (schemas, tables with ownership, columns, constraints, indexes, triggers, enums, functions, views, extensions, the type picker), reads and edits rows by primary key with every value as text, and renders schema changes as goose migrations with Up and Down; `orb dev` serves them at `/_portal/api/db/…` (catalog, `rows/query|insert|import|update|delete`, `ddl/plan|apply`) and applies a written migration through the supervisor. The portal's Table Editor screen uses them. `orb` now depends on pgx.
- Dev Portal phase 1: `devconsole.(*Console).Operator(prefix, actor, logger)`: in development, a `/ops/` request carrying the dev console token as `Authorization: Bearer` acts as the system actor `dev-console` with the platform administrator's permissions, after the console's Host, loopback and token checks; both Full apps mount it after authentication (`devOperator` in `internal/app/devconsole.go`). `orb dev`'s proxy adds the token to `/ops/` requests; `POST /_portal/api/app/migrate` applies pending migrations. The portal's screens read live data: routes with a request builder, requests and logs with live tails, modules, the audit log, jobs, settings, the database and email.
- `cli/internal/genplan`: generators plan before they write. `orb gen job`, `orb gen resource` and `orb gen migration` build a plan of file changes and apply it; a plan is refused when a file it changes was edited since (`ErrStale`) or a file it creates exists (`ErrExists`). Output, `--dry-run` and `--json` are unchanged.

### Live schema status ([ADR-0080](docs/adr/0080-live-schema-status.md))

#### Added

- `orb dev` publishes a `schema` event on `/_portal/api/events` (`Event.Schema`, sent to new streams right after the first `state` event) after every migrate run, whenever a file under `db/migrations` changes and after the SQL editor commits a DDL statement, and serves the same object at `GET /_portal/api/db/schema-status`: `database`, `source` (`startup`, `code`, `migrate`, `portal`, `sql`), `checked_at`, `applied` (what the run applied), `pending` (`file`, `version`, `reason` `new` or `out_of_order`), `edited`, `needs_restart`, `problem` (the last migrate error). `portal.SchemaStatus`, `Hub.SetSchema`, `Hub.Schema`, `portal.ComputeSchemaStatus`, `portal.MigrationRecord`, `pgmeta.IsDDL`.
- `db/migrations` is watched even with `orb dev --no-reload`, on its own snapshot: a changed file that won't be applied on its own (no reload, the app stopped or failed, a failed migrate) is reported in the terminal (`orb: db/migrations changed (… is not applied); restart the app to apply it (Dev Portal → Restart)`) and as `needs_restart` in the status. With `--no-reload` the portal's Restart and migrate requests now run, instead of waiting forever.
- Files edited after they were applied are found: `orb dev` records each applied file's SHA-256 in `.orb/portal/migrations.json` after every successful migrate run (files seen applied for the first time are recorded as they are) and names a mismatch in the terminal and in `edited`, with the fix (Migrations → Redo in development, or a new migration).
- The Dev Portal's Database and Migrations screens show a banner from the status (pending files with Restart or Apply pending, edited files with Redo, out-of-order files, the migrate error), and the Schema, Objects and Table Editor screens refetch on every `schema` event ([guide](docs/guides/dev-portal.md#schema-changes-from-code)).

### Log archive ([ADR-0079](docs/adr/0079-hourly-log-archive.md))

#### Added

- `gorbital.dev/modules/storage/logarchive`: an `slog.Handler` that spools every record as a JSON line into a file for the current hour under a directory and an uploader that gzips each finished hour into the app's `storage.Store` as `logs/<service>/<YYYY>/<MM>/<DD>/<HH>[.<instance>].jsonl.gz` (`.partial-<unix>` for the rest of an hour at shutdown or when switched off), controlled by a live `config.Value[bool]`; `New`, `WithLevel`, `WithInstance`, `WithInterval`, `WithClock`, `(*Archive).Handler`, `Bind`, `Close`, `Status`; failed uploads are logged and retried, and a previous run's files are uploaded at the next start.
- `telemetry.WithLogTee` given more than once sends every record to every handler.
- Full apps: the runtime setting `logs.archive.enabled` (off by default, reason required), `LOG_ARCHIVE_DIR` (default `.orb/logs`), `internal/app/logarchive.go` wiring the archive into the logger and the store; the objects show in `/ops/storage` and the Dev Portal's Storage screen under `logs/`.

### Branded emails ([ADR-0078](docs/adr/0078-branded-email-layout.md))

#### Added

- `mail.Brand` (name, URL, logo, support address, footer line) renders a `mail.Email` (preheader, title, paragraphs, a one-time code block with its label and expiry, a button, closing lines) as HTML and plain text: one 560px column of tables with inline styles in the brand's light palette; `Brand.Message` wraps it in a `mail.Message` with the `category` tag; only `http`, `https` and `mailto` links are rendered. `auth.NewBrandedEmails`, `auth.BrandedEmailPreviews` and `orgs.NewBrandedEmails` take the brand. Full apps build it in `internal/app/mail.go` (`(*App).brand()`, the service name and `APP_PUBLIC_URL`), pass it to both modules and the test message; multi-tenant apps preview the `orgs.invitation` email ([guide](docs/guides/email.md#email-templates)).

#### Changed

- Every auth and organisation email has a title, the code in its own block, the invitation as a button, and a footer with the app's name; `NewMailEmails(sender, appName)` and `EmailPreviews(appName)` remain as the brand with only a name.

### CLI

#### Fixed

- `orb upgrade` with uncommitted changes said to pass `--allow-dirty`, a flag it doesn't have (the upgrade is a commit on its own branch); the message now says to commit or stash first.
