# apistock Roadmap

**Status:** Accepted (2026-09-14) · **Replaces:** `scope-v1.md`

apistock ships through pre-release milestones. Each one is usable on its own and has a definition of done. Nothing outside a milestone's scope starts without an accepted ADR. Everything is v0 until 1.0 ([ADR-0015](adr/0015-public-api-and-stability-tiers.md)).

## Before v0.1: Architecture gate

| Item | Status |
|---|---|
| Architecture v2 and ADRs 0014–0030 | Done |
| OpenAPI spike: code-first with Huma ([ADR-0027](adr/0027-api-contract-and-docs.md), [results](../spikes/openapi/README.md)) | Done |
| Anchor-edit spike: text insertion wins ([ADR-0021](adr/0021-generator-operation-model.md), [results](../spikes/anchor/README.md)) | Done |
| First-run spike: Minimal in 12.0 s cold, 1.6 s warm ([ADR-0028](adr/0028-local-development-environment.md), [results](../spikes/firstrun/README.md)) | Done |
| Merge spike ([ADR-0016](adr/0016-scaffold-compatibility-and-upgrades.md)) | Done |

## v0.1: Foundation

**Status: implemented and tested, not released** (2026-09-14). Results below.

| | |
|---|---|
| **Delivers** | Core packages (`app`, `httpx`, `health`, `actor`, `requestid`, `audit`, `mail`, `config`, `page`, `ratelimit`, `buildinfo`), `modules/openapi` (Huma, problem errors, embedded Scalar), `modules/telemetry` (OpenTelemetry, correlated logs), hand-written `examples/minimal`, Minimal recipe generated from it, `aps new` (Minimal), `aps dev` (reload, `.env`, port check), `aps version`, security defaults, Dockerfile, project CI (tests on Go 1.26/1.27, race, golangci-lint, govulncheck, gitleaks, dependency budget, template and OpenAPI drift, end-to-end), signed release workflow for `aps` |
| **Results** | First run with the real CLI (`scripts/first-run.sh`): **25.0 s** from clean caches, **4.8 s** warm. Generated app passes its own tests. Lint: 0 issues in all modules. `gorelease` starts at the first tag. |
| **Not included** | Database, auth, Full/Custom presets, Docker services |
| **Done when** | On a clean machine: `go install` → `aps new my-api` → `aps dev` → `/docs` in under 60 seconds; generator reproduces `examples/minimal` exactly; threat model rows 2–10 and 21 addressed |

## v0.2: Data and identity

**Status: implemented and tested, not released** (2026-09-15). Done (2026-09-14): `modules/postgres` (pool with tracing, `DBTX`, `InTx`, error classification, goose migrations with advisory lock, readiness check, `pgtest` against Docker PostgreSQL), `modules/settings` and core `config.Value[T]` (typed declarations, PostgreSQL store with version checks, history and audit events, LISTEN/NOTIFY with periodic resync; [ADR-0031](adr/0031-runtime-settings.md)), [ADR-0032](adr/0032-repository-sql.md) repository style; `modules/jobs` library (River client with context propagation and graceful stop, job definitions with runtime-editable config and schedules, `Manager` for the admin panel, `AsyncSender`, River migrations; [ADR-0033](adr/0033-background-jobs.md)). `examples/full-single` golden app wiring postgres, runtime settings and jobs, with `cmd/migrate`, `compose.yaml` and the `/ops/settings`, `/ops/jobs/*` and `/ops/queues` admin APIs behind an interim `OPS_TOKEN` (replaced by platform roles when `modules/auth` lands). `aps gen job` and interactive `aps new`, with every prompt available as a flag ([ADR-0035](adr/0035-interactive-cli.md)). `modules/auditpg` (append-only `audit_events` table, `Record` and transactional `RecordTx`, metadata redaction and size bounds, filtered and paginated `List`, `Get`; [ADR-0036](adr/0036-audit-storage.md)), wired into `examples/full-single` with `GET /ops/audit` and `GET /ops/audit/{id}` (moved from v0.5). Email: `modules/mail/smtp` and `modules/mail/resend`, core `mail.ErrRejected` and `mail.WithDefaults`, rejected sends cancelled by the mail worker, Mailpit in development, sender as runtime settings, `GET /ops/mail` and `POST /ops/mail/test`, and interactive `aps add mail` to choose or switch Resend or SMTP ([ADR-0037](adr/0037-email-setup-and-delivery.md)). Authentication (2026-09-15, [ADR-0038](adr/0038-authentication-v0-2.md)): `modules/auth` building blocks (argon2id hashing, tokens and codes stored as hashes, session middleware and cookies, permission catalog, emails) and, in `examples/full-single`, an app-owned `internal/modules/auth` with domain, use cases, repository (one SQL file per operation) and `/v1/auth` endpoints: register, email codes, login with cookie or bearer token, logout and logout-all, sessions, password reset and change, account deletion, platform roles (`platform_admin`, `ops_viewer`) granted with `go run ./cmd/api grant-role`, and the `auth_cleanup` job. `OPS_TOKEN` is gone: `/ops/*` uses sessions and roles. Example resource (2026-09-15, [ADR-0039](adr/0039-resource-module-template.md)): `internal/modules/projects` in `examples/full-single`, owned by the signed-in user, with all four layers, its own `projects` table and repository, keyset pagination through `page`, versioned `PATCH`, audit events, cross-owner isolation tests and an end-to-end test; the template for `aps gen resource`. `aps gen resource` (2026-09-15): string, text and enum fields with unique and filter options, interactive or flags, reproduces the projects module byte for byte and is tested by generating other resources into a copy of the app and running their tests. Release tracking (2026-09-15, [ADR-0040](adr/0040-release-tracking.md)): `modules/releases` records each instance's build at start with a heartbeat and clean-stop marking, derives releases, and prunes old instances; `examples/full-single` runs the tracker as a worker and serves `GET /ops/releases`, `/ops/releases/current` and `/ops/releases/instances` (moved from v0.5). `aps new --preset=full` (2026-09-15, [ADR-0041](adr/0041-full-preset-generation.md)): templates generated from `examples/full-single` and checked byte for byte, `go.mod` derived from each golden app's `go.mod` for both presets, a leak check for repository paths, the app's database named after the app; a new Full app passes its own tests and accepts `aps gen resource` and `aps gen job`. `aps gen migration` (2026-09-15): an empty forward-only migration that runs after the existing ones, named in any case, with the same flags as the other generators; tested end to end by generating one into a new Full app, adding a column to a generated resource's table and running the app's tests. Seed data (2026-09-15, [ADR-0042](adr/0042-development-seed-data.md)): `cmd/seed` creates `admin@example.com` with `platform_admin`, a random password printed once and never stored, and three example projects, through the modules' use cases; development only and safe to run again. `aps dev` with Docker (2026-09-15, [ADR-0028](adr/0028-local-development-environment.md)): `.env` from `.env.example`, Docker and host port checks naming the `.env` line to change, `docker compose up -d --wait`, migrations (again when a migration changes) and seed data before the app starts; `--no-services`; `--observability` starts Grafana (`grafana/otel-lgtm`, a Compose profile in both presets) and points the app's OpenTelemetry export at it. The Custom preset moved to v0.5 and the tenancy prompt to v0.4.

| | |
|---|---|
| **Delivers** | `modules/postgres`, `modules/settings` (runtime settings declared in code, stored in PostgreSQL, live on every instance, `/ops/settings` API, `config.Value[T]` in core; [ADR-0031](adr/0031-runtime-settings.md)), `modules/jobs` (River, AsyncSender, job definitions with runtime-editable schedule, enabled, timeout and retries; `/ops/jobs` admin APIs; [ADR-0033](adr/0033-background-jobs.md)), `modules/mail/resend` and `mail/smtp`, `modules/auditpg`, `modules/auth` (email/password, email codes, sessions, logout-all, reset/change password, active sessions, delete account, platform roles, permission catalog), `modules/releases`, Full preset with single-tenant generation, `aps dev` with Docker (PostgreSQL, Mailpit, migrations and seed, `--observability` Grafana), `aps gen resource`, `aps gen job` and `aps gen migration`, seed data, `examples/full-single` |
| **Not included** | Custom preset (v0.5), tenancy prompt (v0.4), social login, 2FA, passkeys, multi-tenant, ops APIs other than `/ops/settings`, `/ops/jobs/*`, `/ops/queues`, `/ops/audit`, `/ops/releases` and `/ops/mail` (audit stats and retention stay in v0.5) |
| **Done when** | From a fresh `aps new --preset=full`: register → verify email code → login → role-protected endpoint → audit event recorded, all in e2e tests; a setting changed through `/ops/settings` on one instance is served by a second instance without restart and appears in history and the audit log; a job generated with `aps gen job` can be rescheduled, disabled and run now through `/ops/jobs` without a restart; nullable `org_id` and role-scope columns present in library tables; threat model rows 12, 13, 19, 23 and 24 addressed |
| **Results** | Covered by tests against Docker PostgreSQL and Mailpit: a new Full app (`APS_E2E=1`) passes its own suite, including sign-up, email verification, login, role-protected `/ops/*` endpoints with audit events, the projects resource, seed data, and a setting changed on one instance served by a second with history and an audit event; the generated `heartbeat` job is rescheduled, run and disabled through `/ops/jobs` without a restart (`TestJobsThroughOps`); nullable `org_id` columns exist in the settings, audit and auth tables; threat model rows reviewed against the code ([ADR-0029](adr/0029-threat-model.md)). `aps dev` in a new Full app (`APS_E2E_DOCKER=1`): API ready 9.6 s after start with images and Go caches warm; a cold run with image pulls isn't measured |

## v0.3: Strong authentication

**Status: done** (2026-09-15; threat model rows 14–16 reviewed and marked done, every method covered by its integration tests). Two-factor authentication (2026-09-15, [ADR-0043](adr/0043-two-factor-authentication.md)): TOTP implemented in `modules/auth` (RFC 6238) with an AES-256-GCM `Keyring` from `AUTH_ENCRYPTION_KEYS` and `rotate-auth-keys`, 10 single-use recovery codes, a sign-in challenge (`POST /v1/auth/login` returns 202, `POST /v1/auth/login/mfa` finishes), replay protection across instances, `Catalog.RequireMFA` and core `actor.Require` with step-up permissions, `platform_admin` and `ops_viewer` requiring 2FA in every environment (403 `mfa_required`), `reset-mfa` for lost devices, seed data that enrolls the administrator, and `aps dev` writing a development key to `.env`. Passkeys (2026-09-15, [ADR-0044](adr/0044-passkeys.md)): `modules/auth/passkey` wrapping `go-webauthn` with a `passkeytest` software authenticator; passwordless sign-in and passkeys as a second factor; up to 10 passkeys per account with single-use ceremonies, user verification and clone detection; `WEBAUTHN_*` in the environment with localhost in development; `/.well-known/apple-app-site-association` and `assetlinks.json` for iOS and Android apps; a QR code image in authenticator app setup; after review, changes to sign-in methods need the password once a session's second factor is 10 minutes old, and a passkey confirms account deletion, turning off the authenticator app and replacing recovery codes. Provider setup (2026-09-15, [ADR-0045](adr/0045-sign-in-provider-setup.md)): `AUTH_PROVIDERS.md` in every Full app and a `.env.example` block per method listing what developers provide and where to find it, a **Sign-in methods** block at start, `go run ./cmd/api auth-providers` and `GET /ops/auth/providers`. Google and Apple sign-in (2026-09-15, [ADR-0046](adr/0046-google-and-apple-sign-in.md)): `modules/auth/social` on `x/oauth2` and `go-oidc` with a `socialtest` fake provider; an API-hosted web flow with state bound to a cookie, PKCE and nonce; native ID-token sign-in with server nonces; automatic linking on a verified email that removes an unverified account's password; the second factor still required; identities, Apple token revocation and notifications; `APP_PUBLIC_URL`. CLI look (2026-09-15, [ADR-0035](adr/0035-interactive-cli.md) v0.3 notes): prompts in the apistock theme ([theme](brand/theme.md)); `aps new` asks one question at a time and folds each answer into one line, and prints a log of finished steps ending with `next:`. Follow-ups outside the definition of done: sign-in against real Google and Apple accounts (needs the maintainer's credentials), and Apple token revocation with retries.

| | |
|---|---|
| **Delivers** | Google and Apple sign-in (web and native), account linking, TOTP with recovery codes, passkeys, 2FA policy per role, `docs/auth-providers.md` |
| **Not included** | GitHub login, API keys |
| **Done when** | Each method passes its integration suite; threat model rows 14–16 reviewed and documented |

## v0.4: Organisations

**Status: in progress.** Design accepted in [ADR-0048](adr/0048-organisations-v0-4.md) (2026-09-15). Done: `modules/orgs` (organisation IDs, `RequireMember`, invitation emails); account hooks in the auth module; `examples/full-multi` with the app-owned orgs module (organisations, one role per member, invitations for the invited verified address only, personal workspaces, soft delete, restore and the `orgs_purge` job), org-scoped projects with cross-organisation denial tests, and a drift check against `full-single`; `aps new --tenancy multi`. Next: `aps gen resource --scope org`, then threat model rows for organisations. `aps add orgs` moved to v0.5: it needs the per-feature recipes and 3-way merges that `aps add` and `aps upgrade` bring. A drift check keeps `full-single` and `full-multi` identical outside the files organisations change.

| | |
|---|---|
| **Delivers** | `modules/orgs` (personal workspaces, memberships, invitations, org roles, ownership transfer, soft delete), multi-tenant generation, the tenancy prompt and `--tenancy` flag in `aps new` (moved from v0.2: before organisations its only answer is single-tenant), `resource/org` template (`aps gen resource --scope org`), `examples/full-multi` with a drift check against `full-single` |
| **Not included** | Row-level security, subdomain tenants, per-org billing |
| **Done when** | Generated cross-org denial tests pass for every org-scoped resource; four isolation layers verified |

## v0.5: Operations and upgrades

| | |
|---|---|
| **Delivers** | `/ops/*` (audit stats, system health, jobs overview, retention), maintenance mode, Postman collection, `llms.txt`, `aps upgrade` (3-way merge on a branch), `aps add orgs` (single → multi-tenant, moved from v0.4, [ADR-0048](adr/0048-organisations-v0-4.md)), `aps doctor`, Custom preset (moved from v0.2: it needs the Full golden app split into per-feature recipes with dependency resolution and tested combinations, which `aps add` and `aps upgrade` need too) |
| **Not included** | Feature flags, live observability, incidents |
| **Done when** | An app generated with v0.2 and edited by script upgrades to v0.5 in CI with no lost edits; ops endpoints require platform roles and 2FA |

## v1.0: Stable

| | |
|---|---|
| **Delivers** | External security review with findings fixed, API freeze and stability tiers in force, `apistock.dev` docs site (Mintlify), domain hardening complete (including rate limits shared across instances, replacing today's per-instance limiters), governance and contribution guide |
| **Done when** | Security review signed off; `gorelease` baseline recorded; scaffold compatibility promise ([ADR-0016](adr/0016-scaffold-compatibility-and-upgrades.md)) active |

## v1.1

Feature flags, per-org settings, live observability, incident reports, API keys and service accounts, GitHub login, PostgreSQL row-level security option, idempotency keys, custom local dev console, Resend bounce/complaint webhooks, Prometheus `/metrics` option.

## v1.2: Client templates

**Status: proposed** ([ADR-0047](adr/0047-client-templates.md)).

| | |
|---|---|
| **Delivers** | `aps new` asks for a docs site, a dashboard and mobile apps (`--docs`, `--dashboard`, `--mobile none\|expo\|native`) and creates each from its own template repository, with the app name, bundle ID, API URL and sign-in methods filled in. Templates are pinned, hashed and signed archives listed in each `aps` release, cached locally, with `--templates <path>` for local checkouts. Each template declares what it accepts in `apistock-template.json` and calls the API through clients generated from `api/openapi.json`. Order: fetch and fill-in mechanism with `template-docs`, then `template-dashboard`, then `template-expo` (iOS and Android) |
| **Not included** | Native iOS and Android templates (only when builders ask for them), hybrid apps, upgrading client code with `aps upgrade` (client output is one-shot) |
| **Done when** | CI creates an app with every template against `examples/full-single` and builds each part; a tampered archive or unlisted version is refused; sign-in, 2FA and passkeys work from the dashboard and the Expo app; threat model rows for templates added |

## Later

Native iOS (SwiftUI) and Android (Kotlin) client templates, community module index and author tooling, subdomain tenant resolution, per-org quotas and billing, storage and outgoing webhooks modules, enterprise SSO integration, WASI-sandboxed generators.

## Not planned

Admin web UI inside generated apps (a dashboard comes as a client template instead), hosted control plane, hybrid mobile apps, databases other than PostgreSQL, schema- or database-per-tenant, custom router, ORM or DI container, plugin runtime, secrets or infrastructure configuration stored in the database, storing application logs in PostgreSQL.
