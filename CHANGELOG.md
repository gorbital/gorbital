# Changelog

Notable changes to the gorbital library, the `orb` CLI and generated apps. The library modules and `orb` are versioned together. What existing apps must do for each release is in the [upgrade notes](docs/guides/upgrade-notes.md); why things changed is in the [decision records](docs/adr/README.md).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). `v0.1.0` is the first public release. Until `v1.0.0` there is no compatibility promise between minor versions ([ADR-0015](docs/adr/0015-public-api-and-stability-tiers.md)), though the compatibility checks already run; breaking changes are listed here and in the upgrade notes.

## Unreleased (v0.2.0)

v0.2 turns gorbital into a framework apps import: routes, guards, the middleware stack, sign-in and the operations API move from generated code into the library ([roadmap](docs/v0.2-roadmap.md), [ADR-0081](docs/adr/0081-a-framework-you-import.md)). It is additive: apps created with v0.1.0 keep building against it, and converting to the new layout is optional.

### Added

- Decision records for the v0.2 line: [ADR-0081](docs/adr/0081-a-framework-you-import.md) (a framework you import), [ADR-0082](docs/adr/0082-routes-guards-and-middleware.md) (routes, guards and middleware), [ADR-0083](docs/adr/0083-modules-stack-migrations-and-ejection.md) (modules, the default stack, migrations and ejection), [ADR-0084](docs/adr/0084-versioned-documentation.md) (versioned documentation).
- Versioned documentation: v0.1 stays published unchanged; the v0.2 line is previewed under `/next/`; new Methods and Examples tabs.

### Changed

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
