# Inside a generated app

The functions that make up a Full app's composition root, `internal/app`, and its commands: what each does, what it takes and returns, its side effects and errors, what it touches in the database, and why it's built that way. Read with `examples/full-single/internal/app` open. For the library's packages, see the generated package reference; for one request's path, [life of a request](request-lifecycle.md).

`internal/app` is the only package that reads the environment and the only one that knows every module. Modules receive what they need as constructor arguments ([ADR-0020](../adr/0020-constructors-and-configuration.md), [ADR-0022](../adr/0022-generated-application-layout.md)).

## Startup sequence

```text
cmd/api/main.go  run()
  │
  ├─ LoadConfig(config.OS)                  environment → Config, every error at once
  │    ├─ loadKeyring                         AUTH_ENCRYPTION_KEYS
  │    ├─ loadWebAuthnConfig                  WEBAUTHN_*
  │    ├─ loadSocialConfig                    GOOGLE_*, APPLE_*, APP_PUBLIC_URL
  │    └─ loadMailConfig                      RESEND_API_KEY or SMTP_*   (infra_mail.go)
  │
  ├─ subcommand?  openapi · roles · grant-role · revoke-role · reset-mfa · rotate-auth-keys · auth-providers
  │
  ├─ New(ctx, cfg)
  │    ├─ newBase         telemetry (logger, tracer, meter), health checker, cleanup stack
  │    └─ build
  │         ├─ postgres.Open                  pool; pings; readiness check
  │         ├─ auditpg.NewStore               audit recorder
  │         ├─ declareSettings → settings.NewStore   loads values, starts nothing yet
  │         ├─ newMailSender → jobs.AddMailWorker
  │         ├─ defineJobs → jobs.New → jobs.NewManager
  │         ├─ mail.WithDefaults(jobs.AsyncSender)   the mailer modules use
  │         ├─ authmodule.New                  users, sessions, 2FA, passkeys, Google, Apple
  │         ├─ releases.NewTracker / NewStore
  │         └─ buildHTTP                       mapper, Huma API, modules, health, docs, middleware
  │
  └─ Run(ctx)
       ├─ reportSignInMethods
       └─ lifecycle.Run(server, settings, jobs, jobsManager, releases)
```

## `config.go`

### `LoadConfig(src config.Source) (Config, error)`

| | |
|---|---|
| **Purpose** | Build the whole boot configuration from environment variables |
| **Input** | `config.Source`: `config.OS` in commands; a map in tests |
| **Output** | `Config` with defaults applied, or `invalid configuration:` joining every problem |
| **Side effects** | None: reads only. `_FILE` variants read files |
| **Validation** | Types and ranges; production-only requirements; partial sign-in configuration; mail provider credentials when delivery is `provider`; parses the encryption keys and the Apple key |
| **Why** | A misconfigured deploy should fail on its first start with every problem listed, not one per restart, and never half-start with a feature silently off ([ADR-0045](../adr/0045-sign-in-provider-setup.md)) |

`DATABASE_URL` is read but not required here: `New`, `Migrate` and `Seed` require it, and `WriteOpenAPI` doesn't need a database.

`Config.Production()` reports `APP_ENV == "production"`. `Config.returnOrigins()` is `APP_PUBLIC_URL` plus `APP_CORS_ORIGINS`: where Google and Apple web sign-in may send the browser back.

## `app.go`

### `New(ctx, cfg Config) (*App, error)`

| | |
|---|---|
| **Purpose** | Construct every component in dependency order |
| **Errors** | `DATABASE_URL is required`; `postgres: connect: …`; any constructor's error. On failure, everything already built is closed (`errors.Join(err, cleanup.Close)`) |
| **Database** | Connects and pings; `settings.NewStore` and `jobs.NewManager` read their tables, so an unmigrated database fails here with `relation "…" does not exist` |
| **Concurrency** | Starts no goroutines that outlive it: long-running work only starts in `Run` |
| **Why** | Constructors do setup and fail fast; runners do work ([ADR-0017](../adr/0017-application-lifecycle.md)). Tests call `New` and `Workers` directly |

Components register cleanups as they're created (`cleanup.Add("postgres", …)`), so shutdown closes them in reverse order.

### `newBase(ctx, cfg) (*App, error)`

Sets up OpenTelemetry (`telemetry.Setup`: text logs in development, JSON in production, OTLP export when `OTEL_EXPORTER_OTLP_ENDPOINT` is set), the health checker and the cleanup stack. Needs no infrastructure, which is why `WriteOpenAPI` uses it alone.

### `(*App).build(ctx) error`

The wiring, in order, with the reason for the order:

1. **Pool** first: everything below needs it. Adds `postgres.HealthCheck` to `/readyz`.
2. **Audit recorder**: settings, jobs and auth record events.
3. **Settings store**: its typed values (`appSettings`) feed the mailer and auth durations.
4. **Mail sender and worker**: the worker must be registered before the job client is created.
5. **Job definitions, client, manager**: `defineJobs` receives `authCleanup` as a closure over `a.auth`, which is built next but before any job can run.
6. **Mailer**: `mail.WithDefaults(jobs.AsyncSender(a.jobs), …)` so modules send email by queueing a job, with the sender filled from settings. `warnDefaultSender` logs in production when `mail.from_email` is still the placeholder.
7. **Auth module**, with providers, keyring, passkeys, catalog, emails and the settings-backed durations.
8. **Release tracker and store**.
9. **`buildHTTP`** with the `services` business modules need.

### `(*App).Run(ctx) error`

Creates the HTTP server, prints or logs the sign-in methods, logs `starting`, and hands the server and `Workers()` to `lifecycle.Run`, which runs them until the context ends or a signal arrives, then shuts down: health reports shutting down, drain delay (5 s in production, none in development), graceful stop, cleanup. Returns the first runner error, such as `listen tcp …: bind: address already in use`.

### `(*App).Workers() []lifecycle.Runner`

`settings` (LISTEN/NOTIFY and resync), `jobs` (River client), `jobsManager` (definition changes across instances), `releases` (heartbeat). Exposed so tests can start them without an HTTP server.

### `WriteOpenAPI(ctx, cfg, w) error`

Builds the HTTP layer with a static ping message and no database, and writes the OpenAPI 3.1 document. Used by `api openapi` and the drift check; it's why `buildHTTP` must not need a live pool.

### `Auth()`, `Handler()`, `Close(ctx)`

Accessors for tests and commands: the auth use case service, the full middleware-wrapped handler, and cleanup without running.

## `routes.go`

### `(*App).buildHTTP(svc services) error`

Creates the error mapper and installs Huma's error hooks and pagination mappings; creates the Huma API on a `ServeMux` with bearer auth documented, serving the OpenAPI document only when docs are enabled (`openapi.WithoutSpecEndpoints` otherwise); registers `GET /version` and every module (`registerModules`); mounts `/livez`, `/readyz`, `/docs` (when enabled), the `.well-known` files and the problem 404; then builds the middleware chain. Returns mapping and CORS configuration errors. Order and each middleware: [life of a request](request-lifecycle.md#2-middleware).

### `authLimitKey(r) string`

Returns the client IP for requests the sign-in rate limit applies to (non-GET under `/v1/auth/`, plus provider `start` and `callback`), and `""` for everything else, which the rate limiter skips. Keyed on `RemoteAddr`; see [production](production.md#tls-proxies-and-client-ips).

### `exceptCrossSitePosts(protect) Middleware`

Applies cross-origin protection to every request except `POST /v1/auth/apple/callback` and `POST /v1/auth/apple/notifications`, which Apple posts from its own origin by design. They're protected instead by the single-use state and `__Host-oauth` cookie, and by Apple's signature.

## `modules.go`

### `registerModules(api, mapper, svc) error`

Calls each module's `register<Name>` and joins their errors. `orb gen resource` inserts a line after `//orb:anchor modules`. Each `module_<name>.go` builds the module from `services`, registers its operations and its error mappings: error codes there are public API.

## `jobs.go`, `job_<name>.go`

### `defineJobs(defs, deps jobDeps)`

Declares every job with its code defaults; `orb gen job` adds a line after `//orb:anchor jobs`. Operators override enabled, schedule, timeout, attempts, queue and priority at runtime in `/ops/jobs`, stored in `jobs_definitions` ([ADR-0033](../adr/0033-background-jobs.md)).

| Job | Defined in | Does |
|---|---|---|
| `heartbeat` | `job_heartbeat.go` | Logs a heartbeat every hour; an example to copy or remove |
| `auth_cleanup` | `job_auth_cleanup.go` | Daily at 03:30 UTC: deletes expired sessions, codes and second-factor challenges, and purges accounts deleted longer ago than `auth.deleted_account_retention` (audit action `auth.accounts.purged`). Returns the counts as `authdomain.CleanupResult` |
| `auth_revoke_tokens` | `job_auth_revoke_tokens.go` | Every minute: revokes up to 20 Apple refresh tokens queued in `auth_token_revocations` by unlinking or account deletion; failures back off from 1 minute to 6 hours and are abandoned after 10 attempts (audit action `auth.identity.revocation_abandoned`). Returns the counts as `authdomain.RevocationResult` |
| `ratelimit_cleanup` | `job_ratelimit_cleanup.go` | Hourly: deletes shared rate limit buckets whose keys are back to a full budget (`ratelimitpg.Store.DeleteExpired`, in batches of 10 000) |
| `gorbital.mail.send` | `jobs.AddMailWorker` in `app.go` | Delivers queued email; 8 attempts; `mail.ErrRejected` cancels |

`jobDeps` holds what workers may use. Add a store or client there when a job needs one.

## `settings.go`

### `declareSettings(reg) appSettings`

Declares every runtime setting with type, default, bounds and description, and returns typed handles (`config.Value[T]`) that read the current value on each use. Values live in `settings_values` only when changed; changes arrive on every instance through LISTEN/NOTIFY ([ADR-0031](../adr/0031-runtime-settings.md)).

| Key | Type | Default |
|---|---|---|
| `example.ping_message` | string | `pong` |
| `mail.from_name` | string | the app's name |
| `mail.from_email` | string | `no-reply@example.com` |
| `mail.reply_to` | string | empty |
| `auth.session_idle_ttl` | duration | 14 days |
| `auth.session_absolute_ttl` | duration | 90 days |
| `auth.verification_code_ttl` | duration | 15 minutes |
| `auth.reset_code_ttl` | duration | 30 minutes |
| `auth.deleted_account_retention` | duration | 30 days |

Multi-tenant apps add `orgs.invitation_url`, `orgs.invitation_ttl`, `orgs.deleted_org_retention`, `orgs.max_owned` and `orgs.user_invitations_per_hour`.

`appSettings.mailDefaults()` turns the `mail.*` values into `mail.Defaults` for `mail.WithDefaults`.

## `permissions.go`

### `declarePermissions() *authlib.Catalog`

The permission catalog: every permission modules check, and the platform roles that grant them. `platform_admin` grants every `ops.*` permission; `ops_viewer` the read ones; both require two-factor authentication (`RequireMFA`). `api roles` prints it. Roles are stored per user in `auth_user_roles` and read on every request. Multi-tenant apps also declare organisation permissions after `//orb:anchor org-permissions`.

## `keys.go`

### `loadKeyring(keys config.Secret, production bool) (*authlib.Keyring, error)`

Parses `AUTH_ENCRYPTION_KEYS` with `authlib.ParseKeyring`. Empty: `nil` in development (authenticator apps off, 503 `mfa_unavailable`), an error in production. `Config.keyring()` returns the parsed keyring for `New`, `Seed` and the commands.

## `passkeys.go`

### `loadWebAuthnConfig(get, production) (webAuthnConfig, []error)`

Reads `WEBAUTHN_*`. Development with RP ID and origins empty uses `localhost` and `http://localhost:8080`, `http://localhost:3000`. Errors: RP ID missing while other values are set; origins missing with an RP ID; non-https origins in production; malformed iOS or Android entries. `service()` builds the `passkey.Service` (relying party name `ServiceName`), or `nil` when off.

### `(*App).mountWellKnown(mux)`

When `WEBAUTHN_APPLE_APP_IDS` is set, serves `GET /.well-known/apple-app-site-association` with `{"webcredentials": {"apps": […]}}`; when `WEBAUTHN_ANDROID_APPS` is set, `GET /.well-known/assetlinks.json` with a `delegate_permission/common.get_login_creds` statement per app and its fingerprints. JSON is built once at start.

## `social.go`

### `loadSocialConfig(get, secret, production) (socialConfig, []error)`

Reads Google and Apple. Rules: a Google client ID needs its secret, and mobile client IDs need the web client ID; any Apple value requires Team ID, Key ID, private key, and a Services ID or bundle IDs, and the key must parse; Google or Apple web sign-in requires `APP_PUBLIC_URL` as scheme and host, https in production. Defaults `APP_PUBLIC_URL` to `http://localhost:8080` in development.

### `(socialConfig).providers(ep) (google, apple *social.Provider)`

Builds the `social.Provider`s, or `nil` for a provider that's off. `ep` overrides endpoints in tests (`socialtest`). Google's native audiences are the iOS and Android client IDs, besides the web client ID.

## `mail.go`, `infra_mail.go`

### `newMailSender(cfg) (mail.Sender, error)`

Returns an SMTP sender to Mailpit (`MAILPIT_SMTP_ADDR`, no auth, no TLS) when delivery is `mailpit`, and the provider's sender from `infra_mail.go` otherwise. `orb add mail` replaces `infra_mail.go` (and its `loadMailConfig` and `newProviderSender`) to switch between Resend and SMTP.

### `mailInfo(cfg, settings) opsusecase.MailInfo`

What `GET /ops/mail` shows: provider, delivery, whether credentials are set (never their values), and the sender settings.

## `providers.go`

### `(Config).signInMethods() []opsdomain.SignInMethod`

For each sign-in method, whether it's on and, if not, which variables turn it on. Shared by `GET /ops/auth/providers`, `WriteSignInMethods` (the `auth-providers` command and the development start banner) and `reportSignInMethods` (a log line in production). Never includes values.

## `migrate.go`

### `Migrate(ctx, cfg, w) error`

| | |
|---|---|
| **Purpose** | Bring the schema up to date |
| **Steps** | Open a pool; `postgres.Migrate(ctx, pool, migrations.FS)` under an advisory lock, printing `applied migration <version>`; then `jobs.Migrate` for River, printing `applied job queue migration <n>` |
| **Errors** | Connection errors; the SQL error of a failing migration, which stops there. Goose runs each migration in a transaction unless annotated otherwise, so a failing one leaves nothing half-applied |
| **Concurrency** | Safe to run from several places at once: the advisory lock serializes them |
| **Why** | Schema changes are a release step, never a side effect of starting an instance ([ADR-0017](../adr/0017-application-lifecycle.md)) |

## `seed.go`

### `Seed(ctx, cfg, email, w) error`

| | |
|---|---|
| **Purpose** | Give a development database an administrator and example data ([ADR-0042](../adr/0042-development-seed-data.md)) |
| **Refuses** | `APP_ENV=production`; no `AUTH_ENCRYPTION_KEYS` |
| **Idempotent** | If `email` exists, prints `✓ Seed data is in place` and changes nothing |
| **Creates** | The user with a `crypto/rand.Text()` password and verified email; `platform_admin`; a TOTP enrollment with 10 recovery codes; three projects owned by the administrator |
| **Output** | Password, 2FA key, `otpauth://` URI and recovery codes, once. Only hashes and the encrypted secret are stored |
| **Why through use cases** | Password policy, hashing, encryption and audit events apply exactly as for real sign-ups, as the `seed` system actor |

## `commands.go`, `admin.go`, `admin_mfa.go`

Operator commands share `openCommandDeps(ctx, cfg, name)`, which connects with application name `<ServiceName>-<name>` (visible in `pg_stat_activity`) and builds the audit recorder and auth service. Commands use a stub email sender (`noEmails`) rather than the job queue.

| Function | Command | Does | Audit |
|---|---|---|---|
| `WriteRoles(w)` | `api roles` | Prints roles, descriptions and permissions from `declarePermissions` | — |
| `GrantRole(ctx, cfg, email, role, w)` | `api grant-role <email> <role>` | Adds a platform role to the account; unknown roles and accounts are errors | `auth.role.granted` |
| `RevokeRole(ctx, cfg, email, role, w)` | `api revoke-role <email> <role>` | Removes it; revoking a role the user doesn't have changes nothing | `auth.role.revoked` |
| `ResetMFA(ctx, cfg, email, w)` | `api reset-mfa <email>` | Deletes the account's TOTP secret and recovery codes | `auth.mfa.reset` |
| `RotateAuthKeys(ctx, cfg, w)` | `api rotate-auth-keys` | Decrypts every TOTP secret with whichever key it names and re-encrypts it with the first key in `AUTH_ENCRYPTION_KEYS` | `auth.keys.rotated`, with the key ID and count |
| `WriteSignInMethods(w, cfg)` | `api auth-providers` | Prints the sign-in methods | — |

## Commands

| Command | Package `main` | Calls |
|---|---|---|
| `cmd/api` | `run(ctx, args)` | `LoadConfig`, then a subcommand or `New` + `Run`. Errors print as `<app>: <error>` with exit code 1 |
| `cmd/migrate` | `run(ctx)` | `LoadConfig`, `Migrate`. Errors: `migrate: <error>` |
| `cmd/seed` | `run(ctx)` | `LoadConfig`, `Seed(DefaultSeedEmail)`. Errors: `seed: <error>` |

Each reads the process environment through `config.OS`; none reads `.env`.
