# ADR-0089: Sign-in profiles

**Status:** Proposed (2026-09-21) · **Amends:** ADR-0024 · **Builds on:** ADR-0081, ADR-0083 · **Related:** ADR-0090, ADR-0092

## Context

Sign-in is all or nothing. An app that wants an email address and a password gets passkeys, authenticator apps, Google, Apple, GitHub, API keys and service accounts as well: their routes in its OpenAPI document, their settings in `/ops/settings`, their rate limiters in `/ops/auth/rate-limits`, their permissions in the catalog, their tables in its database. Nothing is wrong with any of it. It is simply not what the developer asked for, and ADR-0092 says the framework owns mechanism, never the app's product.

The code is already divided the right way. Each method has its own delivery file, its own use cases and its own migration; what is missing is a way for the app to say which of them it wants.

Facts checked on 2026-09-21 against this worktree:

| Area | Today | Evidence |
|---|---|---|
| Operations | 74, registered unconditionally, split by file: `auth.go` 12, `registration.go` 1, `ops_users.go` 16, `apikeys.go` 19, `social.go` 12, `passkeys.go` 9, `mfa.go` 5 | `grep -c 'OperationID: "'` over `gorbital/authhttp/internal/delivery/` |
| The one entry point | `func Register(router *gorbital.Router, svc *authusecase.Service, c Config)` | `gorbital/authhttp/internal/delivery/auth.go:193` |
| What can be gated | `delivery.Config` has four fields: `Cookie`, `Middleware`, `Registration`, `MinPasswordLength` | `gorbital/authhttp/internal/delivery/auth.go:173` |
| What the module wires | `Settings`, `Jobs`, `Migrations`, `RateLimiters`, `Retention`, `Routes`, plus `Errors` and `Permissions` | `gorbital/authhttp/module.go`, `Authenticator.Module` |
| Migrations | `moduleMigrations()` returns 8, from `auth` to `auth_bans`, under the versions v0.1 apps hold them | `gorbital/authhttp/module.go` |
| Migration history | `goose.NewProvider(…, goose.WithSessionLocker(locker))` — no `goose.WithAllowMissing` | `modules/postgres/migrate.go:159` |
| Library versions | `libraryMigrations` is a frozen list; "Released versions never change" | `gorbital/migrate.go` |
| Existing options | eleven, none of them about which methods exist: `MinPasswordLength`, `PasswordPolicy`, `RequireMFA`, `APIKeyMaxTTL`, `WithoutRegistration`, `Brand`, `RouteMiddleware`, `BeforeLogin`, `AfterLogin`, `OnRegister`, `RegisterFields` | `gorbital/authhttp/options.go` |
| Reporting | `SignInMethod{Key, Name, Enabled, Detail, Missing, Guide}`; `signInMethods` reads configuration only | `gorbital/platform.go`, `gorbital/authhttp/providers.go` |
| Rate limiters | 7, declared as one list for every method | `gorbital/authhttp/limits.go:41` |

Two things follow. Gating *registration* per method is mechanical, because the files already draw the lines. Gating *migrations* per method is not, because `goose` here refuses an out-of-order file: a migration left out at creation could never be applied afterwards.

## Options

### How an app chooses its methods

| Option | Verdict |
|---|---|
| A build tag per method | Rejected: invisible in `main.go`, untestable in one `go test ./...`, and a wrong tag fails at link time with no useful message |
| Separate `authbasic` and `authfull` packages | Rejected: two packages, two API listings and two copies of the same flows, for one axis with six values |
| **`Methods(...Method)`, one variadic option, defaulting to every method** | **Chosen**: the default is today's behaviour exactly, the choice is one readable line, and a wrong set fails `New` |

### What a method set gates

| Option | Verdict |
|---|---|
| Routes only | Rejected: the settings, jobs, limiters and permissions of an absent method stay in `/ops`, so the app still describes features it does not have |
| Routes and the OpenAPI document, by filtering the document afterwards | Rejected: the operations still exist and still answer; only the description of them would shrink |
| **Everything the module declares — routes, settings, jobs, rate limiters, permissions, retention — so the document shrinks because the routes are absent** | **Chosen** |

### Migrations

| Option | Verdict |
|---|---|
| Gate migrations with the methods | Rejected: `goose` is built without `AllowMissing`, so `20260915000005_auth_passkeys.sql` skipped at `orb new` can never be applied later. The failure appears in a user's production database, at the moment they try to add a feature |
| Renumber a method's migration when it is turned on | Rejected: `libraryMigrations` and `moduleMigrations` are frozen version lists. Renumbering means two apps with the same schema and different histories |
| **Apply all eight whatever the method set; the unused tables exist and stay empty** | **Chosen**: a few empty tables in exchange for a history that can never diverge, and for turning a method on later being a one-line change |

### A disabled method's configuration

| Option | Verdict |
|---|---|
| Fail `CheckConfig` as today | Rejected: an app that sets `GOOGLE_CLIENT_ID` in a shared `.env` and does not enable social sign-in would not start |
| Ignore the configuration silently | Rejected: the developer believes they configured Google sign-in, and nothing says otherwise |
| **Warn, and report the method as not enabled in this app** | **Chosen** |

## Decision

### 1. `Methods` is an option

```go
type Method string

const (
	MethodPassword  Method = "password"  // 13 operations: auth.go 12 + registration.go 1
	MethodOperators Method = "operators" // 16: /ops/auth/users
	MethodTOTP      Method = "totp"      //  5
	MethodPasskeys  Method = "passkeys"  //  9
	MethodSocial    Method = "social"    // 12: Google, Apple, GitHub
	MethodAPIKeys   Method = "api_keys"  // 19: keys and service accounts
)

// Methods chooses the sign-in methods the app serves. Without it, every
// method is served, which is v0.2 behaviour byte for byte.
func Methods(m ...Method) Option
```

A set without `MethodPassword` fails `New`, naming the option: `authhttp` is the password implementation, and an app that does not want passwords wants a different authenticator, not this one with a hole in it. An unknown `Method` fails `New` too.

### 2. Everything the module declares is filtered

`Authenticator.Module` filters `Settings`, `Jobs`, `RateLimiters`, `Permissions`, `Retention` and `Routes` by the set. The OpenAPI document shrinks as a consequence of the routes being absent, not by a second mechanism: there is one source of truth for what the app serves.

`delivery.Register` splits along the files as they already are:

```go
func Register(router *gorbital.Router, svc *usecase.Service, c Config) {
	r, h := routesOn(router), &handler{svc: svc, cookie: c.Cookie}
	registerPassword(r, h, c) // always
	if c.Methods.Has(MethodOperators) { registerOperators(r, h, c) }
	if c.Methods.Has(MethodTOTP)      { registerMFA(r, h, c) }
	if c.Methods.Has(MethodPasskeys)  { registerPasskeys(r, h, c) }
	if c.Methods.Has(MethodSocial)    { registerSocial(r, h, c) }
	if c.Methods.Has(MethodAPIKeys)   { registerAPIKeys(r, h, c) }
}
```

`Config` gains the set beside `Cookie`, `Middleware`, `Registration` and `MinPasswordLength`. Error mappings are **not** filtered: they are a static table of codes, they cost nothing, and a code that disappears from a build is a worse surprise than one that never fires.

### 3. Migrations are not gated

Every app with `authhttp` applies all eight of `moduleMigrations()`, whatever its method set. A `basic` app has `auth_passkeys` and `auth_social` tables, and they stay empty.

This is the release's central trade-off, and it is taken deliberately. `modules/postgres` builds `goose.NewProvider` without `goose.WithAllowMissing`, so the history is strictly ordered: a file skipped at creation is a file that can never be applied. Gating migrations would buy two unused tables now and cost a failed migration in production later, at the exact moment a developer tries to grow their app. Empty tables are cheap; a divergent history is not.

### 4. Configuration of a disabled method warns

`CheckConfig` does not fail on a half-configured provider whose method is off — it warns, naming the method and the option that would turn it on. `SignInMethods()` gains the third answer it lacks today:

| State | Reported as |
|---|---|
| Enabled in this app and configured | `Enabled: true`, with `Detail` |
| Enabled in this app, not configured | `Enabled: false`, with `Missing` and `Guide`, as today |
| Not enabled in this app | `Enabled: false`, `Missing` empty, `Detail` naming `authhttp.Methods` |

### 5. Three CLI profiles

| `--auth` | `main.go` | Operations | Auth migrations |
|---|---|---:|---|
| `none` | no auth module | 0 | none |
| `basic` | `authhttp.New(authhttp.Methods(MethodPassword, MethodOperators))` | 29 | all 8 |
| `full` | `authhttp.New()` | 74 | all 8 |

`none` has no `authhttp` at all, so its migrations are never in the app's history — that is a different case from omitting one of eight.

### 6. External identity is not a profile in v0.3

`modules/jwt` already satisfies `gorbital.Authenticator`, which has exactly one method — `Middleware(*slog.Logger) func(http.Handler) http.Handler` (`gorbital/options.go:39`) — and works today wired by hand. The `--auth external` profile, tenancy read from claims, `JWT_*` configuration and the Auth0 and Clerk guides are v0.4 (D14).

Deferring costs nothing, and that is checkable rather than hopeful: nothing in this record has to change for it. `--auth external` is a fourth row in the table above and a scope authorizer built from claims. The `Methods` option, the migration rule and the reporting are unaffected, because an app with an external identity provider does not mount `authhttp` at all.

### 7. Turning a method on after creation is not in v0.3

`orb add auth` and `orb add passkeys` need out-of-order migration support, a rollback story and their own ADR (D7). What decision 3 buys is that, until they exist, turning a method on is still possible by hand: add the constant to the `Methods` call in `main.go` and restart. One line, no migration, because the tables are already there. That is precisely why decision 3 was made.

### 8. A disabled method's code still ships

Honestly recorded, because it is the limit of this release: `orb new` writes `authhttp` into the app (ADR-0092), and a `basic` app holds the passkey, social and API-key files whether or not it serves them. Delivery, use cases and ports are interlinked; deleting the files would not compile. What shrinks is the routes, the settings, the jobs, the rate limiters, the permissions and the OpenAPI document — what the app *exposes*, not what it *contains*. Splitting `authhttp` into per-method packages would fix this and is future work, not v0.3 (D6).

## Threat model

| Threat | Mitigation |
|---|---|
| A disabled method's path answers, revealing the feature exists | The route is never registered, so the path is 404 from the router, not 401 or 500 from a handler that ran. A 401 would say "this exists, authenticate"; a 500 would say "this exists and is broken" |
| A role is granted a permission for a method the app does not serve | `permissions()` is filtered with the routes, so the permission is absent from the catalog. A grant naming it fails `New`, rather than sitting unused until the method is turned on |
| The empty `auth_passkeys` or `auth_social` tables are reachable | No registered route reads or writes them: the use cases that do are only constructed by the `register*` functions that are skipped. A golden test asserts that a `basic` app serves exactly 29 operations and that none of them touch those tables |
| The OpenAPI document advertises what the app does not serve | The document is generated from the registered routes, so it cannot disagree with them. Each profile has its own baseline in CI |
| A half-configured provider looks enabled to an operator | `SignInMethods()` distinguishes "not enabled in this app" from "not configured", and `/ops/auth/providers` prints the distinction |
| A disabled method is silently re-enabled by an upgrade | The method set is in `main.go`, in the app's repository, under review like any other line. The default is every method, so an app that never calls `Methods` keeps v0.2 behaviour |

## Consequences

- An app chooses its sign-in surface in one line, and its OpenAPI document, settings, jobs, limiters and permissions follow.
- A `basic` app serves 29 of 74 operations and carries eight migrations, two of which create tables it does not use. Accepted, for the reason in decision 3.
- Every v0.2 app is unchanged: no `Methods` call means every method, and the `full` profile's document is byte-identical to v0.2.1. That equality is a release gate.
- `delivery.Register` becomes six functions instead of one. The files do not move, so the change is reviewable as a diff of registration calls.
- Each profile needs its own OpenAPI baseline and its own golden app, which is three, not sixteen (ADR-0090).
- `authhttp` keeps shipping every method's code in every app. The claim "you only get what you asked for" is true of the app's HTTP surface and false of its source tree, and the documentation must say so.
- `orb add auth` stays out of reach until out-of-order migrations are decided, but the manual path — one line in `main.go` — works from the first release.
