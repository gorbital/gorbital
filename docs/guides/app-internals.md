# Inside a generated app

The functions that make up the composition root of a Full app on the v0.1 layout, `internal/app`, and its commands: what each does, what it takes and returns, its side effects and errors, what it touches in the database, and why it's built that way. Read with `examples/v0.1/full-single/internal/app` open.

> Apps created by orb v0.1 have this layout and keep it until `orb upgrade --layout v0.2`. A new Full app runs on `gorbital.Main` and has no `internal/app`: the library builds what this page describes ([Your main.go](main-go.md), [The middleware stack](middleware-stack.md), [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md#phase-9-implementation-notes-new-apps-on-the-v02-layout-2026-09-17)), and `cmd/api/main.go` says in one line each what it contains. For the library's packages, see the generated package reference; for one request's path, [life of a request](request-lifecycle.md).

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
  │         ├─ declareFlags → flags.NewStore         loads feature flag states
  │         ├─ newMailSender → jobs.AddMailWorker
  │         ├─ defineJobs → jobs.New → jobs.NewManager
  │         ├─ mail.WithDefaults(jobs.AsyncSender)   the mailer modules use
  │         ├─ authmodule.New                  users, sessions, 2FA, passkeys, Google, Apple
  │         ├─ releases.NewTracker / NewStore
  │         └─ buildHTTP                       mapper, Huma API, modules, health, docs, middleware
  │
  └─ Run(ctx)
       ├─ reportSignInMethods
       └─ lifecycle.Run(server, settings, flags, jobs, jobsManager, releases)
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
3. **Settings store**: its typed values (`appSettings`) feed the mailer and auth durations. Then the **feature flags store** (`appFlags`), whose flags feed modules such as ping.
4. **Mail sender and worker**: the worker must be registered before the job client is created.
5. **Job definitions, client, manager**: `defineJobs` receives `authCleanup` as a closure over `a.auth`, which is built next but before any job can run.
6. **Mailer**: `mail.WithDefaults(jobs.AsyncSender(a.jobs), …)` so modules send email by queueing a job, with the sender filled from settings. `warnDefaultSender` logs in production when `mail.from_email` is still the placeholder.
7. **Auth module**, with providers, keyring, passkeys, catalog, emails and the settings-backed durations.
8. **Release tracker and store**.
9. **`buildHTTP`** with the `services` business modules need.

### `(*App).Run(ctx) error`

Creates the HTTP server, prints or logs the sign-in methods, logs `starting`, and hands the server and `Workers()` to `lifecycle.Run`, which runs them until the context ends or a signal arrives, then shuts down: health reports shutting down, drain delay (5 s in production, none in development), graceful stop, cleanup. Returns the first runner error, such as `listen tcp …: bind: address already in use`.

### `(*App).Workers() []lifecycle.Runner`

`settings` and `flags` (LISTEN/NOTIFY and resync), `jobs` (River client), `jobsManager` (definition changes across instances), `releases` (heartbeat). Exposed so tests can start them without an HTTP server.

### `WriteOpenAPI(ctx, cfg, w) error`

Builds the HTTP layer with a static ping message and no database, and writes the OpenAPI 3.1 document. Used by `api openapi` and the drift check; it's why `buildHTTP` must not need a live pool.

### `Auth()`, `Handler()`, `Close(ctx)`

Accessors for tests and commands: the auth use case service, the full middleware-wrapped handler, and cleanup without running.

## `routes.go`

### `(*App).buildHTTP(svc services) error`

Creates the error mapper and installs Huma's error hooks and pagination mappings; creates the Huma API on a `ServeMux` with bearer auth documented, serving the OpenAPI document only when docs are enabled (`openapi.WithoutSpecEndpoints` otherwise); registers `GET /version` and every module (`registerModules`); mounts `/livez`, `/readyz`, `/docs` (when enabled), the `.well-known` files and the problem 404; then builds the middleware chain. Returns mapping and CORS configuration errors. Order and each middleware: [life of a request](request-lifecycle.md#2-middleware).

### `authLimitKey(r) string`

Returns the client IP for requests the sign-in rate limit applies to (non-GET under `/v1/auth/`, plus provider `start` and `callback`), and `""` for everything else, which the rate limiter skips. Keyed by `ratelimit.ByRemoteIP`: the IPv4 address or IPv6 /64 of `RemoteAddr`, which `httpx.TrustedProxies` has already set to the client behind `APP_TRUSTED_PROXIES`; see [production](production.md#tls-proxies-and-client-ips).

### `exceptCrossSitePosts(protect) Middleware`

Applies cross-origin protection to every request except `POST /v1/auth/apple/callback` and `POST /v1/auth/apple/notifications`, which Apple posts from its own origin by design. They're protected instead by the single-use state and `__Host-oauth` cookie, and by Apple's signature.

## `idempotency.go`

### `newIdempotency(pool, settings, logger)`, `idempotencyMiddleware(store)`, `documentIdempotencyKey(api)`

Builds the `modules/idempotency` store with `idempotency.retention`, the middleware that replays retried POST and PATCH requests with an `Idempotency-Key` for the signed-in caller (skipping `/v1/auth/`), and adds the optional header to those operations in the OpenAPI document. See the [idempotency guide](idempotency.md) and [ADR-0060](../adr/0060-idempotency-keys.md).

## `observability.go`

### `newObservabilityStore(pool)`, `newCollector(store, instanceID, logger)`, `newStreams()`, `observabilityMiddleware(collector)`, `reauthenticate(auth)`, `detectIncidents(store, settings)`

Builds the `modules/observability` store of request minutes and incidents; this instance's collector, which counts requests under the release tracker's instance ID and writes them every 15 seconds (it runs with the workers); the live stream limits (2 per user, 20 per instance, 10 minutes); the request-count middleware (nothing when exporting the OpenAPI document); the session check streams repeat before each event; and the `incidents_detect` job's detection with the `incidents.*` settings. See the [observability guide](observability.md) and [ADR-0064](../adr/0064-live-observability-and-incidents.md).

## `devconsole.go`

### `loadDevConsoleConfig(get, secret, production)`, `newDevConsoleLogs(cfg)`, `buildDevConsole(pool)`, `devApp`, `devRoutes`, `devJobRuns`, `handlerRoutes`

Reads `DEV_CONSOLE_TOKEN` (refused in production, at least 32 characters) and `MAILPIT_WEB_PORT`. When the app runs in development with a token: a buffer of recent log records that `telemetry.WithLogTee` fills, and a `modules/devconsole` console subscribed to the request collector, with sources for the app's wiring, routes (the OpenAPI document plus `handlerRoutes`), the variables `LoadConfig` recorded in `Config.EnvKeys`, Mailpit, migrations and job runs. `buildHTTP` mounts it in front of the middleware; `Run` ends its streams on shutdown. Otherwise nothing is built and nothing changes. See [dev console APIs](dev-console.md) and [ADR-0065](../adr/0065-local-dev-console-apis.md).

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
| `idempotency_cleanup` | `job_idempotency_cleanup.go` | Hourly: deletes idempotency keys and their stored responses older than `idempotency.retention` (`idempotency.Store.DeleteExpired`, in batches of 1 000) |
| `observability_cleanup` | `job_observability_cleanup.go` | Hourly: deletes request minutes older than `observability.retention` (`observability.Store.DeleteBefore`, in batches of 5 000) |
| `incidents_detect` | `job_incidents_detect.go` | Every minute, one attempt: opens an automatic incident when the server error rate over `incidents.detection_window` is above `incidents.error_rate_threshold` with at least `incidents.min_requests` requests, and notes recovery; logs, counts `incidents.detections` and records `ops.incident.opened` or `ops.incident.updated` |
| `gorbital.mail.send` | `jobs.AddMailWorker` in `app.go` | Delivers queued email; 8 attempts; `mail.ErrRejected` cancels |

`jobDeps` holds what workers may use. Add a store or client there when a job needs one.

## `flags.go`

### `declareFlags(reg) appFlags`

Declares every feature flag and returns the handles; `example.ping_time` (a client flag) adds `server_time` to `GET /v1/ping` replies. States live in `flags_states` only when changed through `/ops/flags`, and changes arrive on every instance through LISTEN/NOTIFY. `module_flags.go` wires `GET /v1/flags`; in multi-tenant apps the orgs module serves `GET /v1/orgs/{orgId}/flags`. See the [feature flags guide](feature-flags.md) and [ADR-0057](../adr/0057-feature-flags.md).

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
| `auth.unverified_account_ttl` | duration | 7 days |
| `auth.ip_requests_per_minute` | int | 60 |
| `auth.login_attempts` | int | 10 |
| `auth.login_address_attempts` | int | 50 |
| `auth.login_window` | duration | 15 minutes |
| `auth.mfa_change_attempts` | int | 10 |
| `auth.reauth_attempts` | int | 10 |
| `auth.code_attempts` | int | 20 |
| `auth.code_window` | duration | 24 hours |
| `audit.retention` | duration | 365 days |
| `ops.history_retention` | duration | 365 days |
| `releases.instance_retention` | duration | 90 days |
| `maintenance.enabled` | bool | `false` |
| `maintenance.message` | string | empty |
| `maintenance.retry_after` | duration | 5 minutes |

Multi-tenant apps add `orgs.invitation_url`, `orgs.invitation_ttl`, `orgs.deleted_org_retention`, `orgs.max_owned` and `orgs.user_invitations_per_hour`. Bounds and which changes need a reason: [ops API](ops-api.md#runtime-settings).

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

Returns an SMTP sender to `orb dev`'s mail catcher (`DEV_MAIL_SMTP_ADDR`, no auth, no TLS) when delivery is `devmail`, to Mailpit (`MAILPIT_SMTP_ADDR`) when it is `mailpit`, and the provider's sender from `infra_mail.go` otherwise. `orb add mail` replaces `infra_mail.go` (and its `loadMailConfig` and `newProviderSender`) to switch between Resend and SMTP.

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
