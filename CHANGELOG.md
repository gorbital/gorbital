# Changelog

Notable changes to the gorbital library, the `orb` CLI and generated apps. The library modules and `orb` are versioned together. What existing apps must do for each release is in the [upgrade notes](docs/guides/upgrade-notes.md); why things changed is in the [decision records](docs/adr/README.md).

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Everything is v0 until 1.0 ([ADR-0015](docs/adr/0015-public-api-and-stability-tiers.md)).

## Unreleased

Nothing here is tagged yet. v1.0 (stable) and the first v1.1 features are both on the development branch; each keeps its own section so the v1.0 release notes stay separate.

### v1.1: Operations and integrations

#### Added

- Per-organisation settings ([ADR-0056](docs/adr/0056-per-organisation-settings.md)): `settings.OrgOverridable()`; organisation values returned by `Setting.Get` from the context; `Store.ListForOrg`, `GetForOrg`, `SetForOrg`, `ResetForOrg`, `HistoryForOrg`, `Overrides`; `settings.ErrNotOrgOverridable`, `settings.ErrInvalidOrgID`; `View.OrgOverridable`, `View.OrgID`, `View.PlatformValue`, `HistoryEntry.OrgID`.
- Multi-tenant Full apps: `GET /v1/orgs/{orgId}/settings`, `GET|PUT|DELETE /v1/orgs/{orgId}/settings/{key}`, `GET /v1/orgs/{orgId}/settings/{key}/history`; organisation permissions `orgs.settings.read` and `orgs.settings.write`; `orgs.invitation_ttl` can be set per organisation. All Full apps: `GET /ops/settings/{key}/overrides` and `org_overridable` in setting responses.
- `modules/idempotency` ([ADR-0060](docs/adr/0060-idempotency-keys.md), [guide](docs/guides/idempotency.md)): `Idempotency-Key` on POST and PATCH requests, stored per caller in PostgreSQL and replayed with `Idempotent-Replayed: true`; 422 `idempotency_key_reused` for a different request, 409 `idempotency_in_progress` while running, 400 `invalid_idempotency_key`; server errors and `idempotency.DontStore` release the key.
- Full apps: the idempotency middleware after authentication (skipping `/v1/auth/*`), the header documented on POST and PATCH operations, setting `idempotency.retention`, job `idempotency_cleanup`, `idempotency_keys` in `/ops/retention`; CORS allows `Idempotency-Key` and exposes `Idempotent-Replayed`.
- Email suppression list and Resend webhooks ([ADR-0062](docs/adr/0062-resend-webhooks-and-suppression-list.md)): `mail.SuppressionList`, `WithSuppressionList`, `ErrSuppressed` (wraps `ErrRejected`) and `NormalizeAddress`; `resend.VerifyWebhook`, `CheckWebhookSecret`, `ParseWebhookEvent`, `ErrInvalidWebhook`, `ErrWebhookTimestamp`, `WebhookTolerance`; new `modules/mail/suppressionpg`.
- Full apps: `POST /v1/webhooks/resend` (with `RESEND_WEBHOOK_SECRET`), the mail worker skipping suppressed addresses, `GET /ops/mail/suppressions`, `DELETE /ops/mail/suppressions/{id}`, permission `ops.mail.write`, audit actions `mail.suppression.added` and `mail.suppression.removed`; `GET /ops/mail` reports `webhook_secret` for Resend; `orb add mail --provider resend` adds `RESEND_WEBHOOK_SECRET` to `.env.example` and a next step for the webhook.
- Prometheus metrics ([ADR-0063](docs/adr/0063-prometheus-metrics.md)): `METRICS_ADDR` serves `GET /metrics` on a separate listener, off by default, in every preset; `telemetry.WithPrometheus`, `(*Telemetry).MetricsHandler`.
- Go runtime metrics (`telemetry.WithRuntimeMetrics`) and PostgreSQL pool metrics (`postgres.WithMeterProvider`), exported over OTLP and Prometheus; `telemetry.RecordRoute` gives spans and HTTP metrics the matched route pattern.

#### Changed

- `orb add orgs` also copies organisation-only migrations added after organisations shipped (`recipes.OrgsLaterMigrationPaths`).
- `modules/telemetry` depends on the OpenTelemetry Prometheus exporter and runtime instrumentation (and `prometheus/client_golang`).

#### Fixed

- Full apps' HTTP server spans were named after the method only (`GET`) and HTTP metrics had no `http.route`, because the session middleware passes a copy of the request to the mux; `routes.go` now wraps the mux with `telemetry.RecordRoute`.

### v1.0: Stable

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

## v0.5.0 (2026-09-15)

### Added

- `orb upgrade`: 3-way merges of template changes on a branch, with the merge base rebuilt from the recorded release and proven against `gorbital.lock` v2 ([ADR-0050](docs/adr/0050-upgrades-and-adding-features.md)).
- `orb add orgs`: turns a single-tenant Full app multi-tenant, with migrations that move existing data.
- `orb doctor`.
- Ops endpoints: `GET /ops/system`, `/ops/audit/stats`, `/ops/jobs/overview`, `/ops/retention`; retention settings and the daily `retention` job; maintenance mode ([ADR-0051](docs/adr/0051-operations-v0-5.md)).
- `api/postman_collection.json` and `api/llms.txt`, generated with the OpenAPI document.

### Changed

- Audit events older than 365 days are deleted by default.

## v0.4.0 (2026-09-15)

### Added

- Multi-tenant organisations: `modules/orgs`, `orb new --tenancy multi`, personal workspaces, memberships and org roles, invitations, soft delete, restore and purge ([ADR-0048](docs/adr/0048-organisations-v0-4.md)).
- `orb gen resource --scope org` with generated cross-organisation denial tests.

## v0.3.0 (2026-09-15)

### Added

- Two-factor authentication with authenticator apps and recovery codes; 2FA required for ops roles ([ADR-0043](docs/adr/0043-two-factor-authentication.md)).
- Passkeys, for passwordless sign-in and as a second factor ([ADR-0044](docs/adr/0044-passkeys.md)).
- Google and Apple sign-in, web and native ([ADR-0046](docs/adr/0046-google-and-apple-sign-in.md)); sign-in provider setup and `AUTH_PROVIDERS.md` ([ADR-0045](docs/adr/0045-sign-in-provider-setup.md)).

## v0.2.0 (2026-09-15)

### Added

- The Full preset: PostgreSQL, runtime settings, background jobs on River, email through Resend or SMTP, audit log, email and password authentication, platform roles and permissions, ops APIs, seed data.
- `orb dev` with Docker services, `orb gen resource`, `orb gen job`, `orb gen migration`, `orb add mail`.

## v0.1.0 (2026-09-14)

### Added

- Core packages, OpenAPI with Huma, OpenTelemetry, the Minimal preset, `orb new`, `orb dev`, `orb version`, project CI and the signed release workflow.
