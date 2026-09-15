# apistock Roadmap

**Status:** Accepted (2026-09-14) · **Replaces:** `scope-v1.md`

apistock ships through pre-release milestones. Each one is usable on its own and has a definition of done. Nothing outside a milestone's scope starts without an accepted ADR. Everything is v0 until 1.0 ([ADR-0015](adr/0015-public-api-and-stability-tiers.md)).

## Where things stand

| Milestone | Theme | Status | What you can use today |
|---|---|---|---|
| [v0.1](#v01-foundation) | Foundation | ✅ Done | `aps new` (Minimal), `aps dev`, core library, `/docs` |
| [v0.2](#v02-data-and-identity) | Data and identity | ✅ Done, tagged `v0.2.0` | Full preset: PostgreSQL, settings, jobs, email, sign-in, roles, audit, `aps gen` |
| [v0.3](#v03-strong-authentication) | Strong authentication | ✅ Done, tagged `v0.3.0` | Authenticator apps, passkeys, Google and Apple sign-in |
| [v0.4](#v04-organisations) | Organisations | ✅ Done, tagged `v0.4.0` | `aps new --tenancy multi`, org-scoped resources |
| [v0.5](#v05-operations-and-upgrades) | Operations and upgrades | ✅ Done, tagged `v0.5.0` | `aps upgrade`, `aps add orgs`, `aps doctor`, ops endpoints, maintenance mode |
| [v1.0](#v10-stable) | Stable | 🔨 In progress | Rate limits shared across instances, trusted proxies |
| [v1.1](#v11) | Operations and integrations | Planned | |
| [v1.2](#v12-client-templates) | Client templates | Proposed | |
| [v1.3](#v13-public-website) | Public website | 🔨 Built early, published | apistock.dev, docs.apistock.dev |

Each milestone below has the same parts: **status** with what was built, then a table of what it **delivers**, what's **not included**, when it's **done**, and measured **results**.

## Before v0.1: Architecture gate

| Item | Status |
|---|---|
| Architecture v2 and ADRs 0014–0030 | Done |
| OpenAPI spike: code-first with Huma ([ADR-0027](adr/0027-api-contract-and-docs.md), [results](../spikes/openapi/README.md)) | Done |
| Anchor-edit spike: text insertion wins ([ADR-0021](adr/0021-generator-operation-model.md), [results](../spikes/anchor/README.md)) | Done |
| First-run spike: Minimal in 12.0 s cold, 1.6 s warm ([ADR-0028](adr/0028-local-development-environment.md), [results](../spikes/firstrun/README.md)) | Done |
| Merge spike ([ADR-0016](adr/0016-scaffold-compatibility-and-upgrades.md)) | Done |

## v0.1: Foundation

**Status: done** (2026-09-14).

| | |
|---|---|
| **Delivers** | Core packages (`app`, `httpx`, `health`, `actor`, `requestid`, `audit`, `mail`, `config`, `page`, `ratelimit`, `buildinfo`), `modules/openapi` (Huma, problem errors, embedded Scalar, since replaced by the apistock reference in v1.3), `modules/telemetry` (OpenTelemetry, correlated logs), hand-written `examples/minimal`, Minimal recipe generated from it, `aps new` (Minimal), `aps dev` (reload, `.env`, port check), `aps version`, security defaults, Dockerfile, project CI (tests on Go 1.26/1.27, race, golangci-lint, govulncheck, gitleaks, dependency budget, template and OpenAPI drift, end-to-end), signed release workflow for `aps` |
| **Results** | First run with the real CLI (`scripts/first-run.sh`): **25.0 s** from clean caches, **4.8 s** warm. Generated app passes its own tests. Lint: 0 issues in all modules. `gorelease` starts at the first tag. |
| **Not included** | Database, auth, Full/Custom presets, Docker services |
| **Done when** | On a clean machine: `go install` → `aps new my-api` → `aps dev` → `/docs` in under 60 seconds; generator reproduces `examples/minimal` exactly; threat model rows 2–10 and 21 addressed |

## v0.2: Data and identity

**Status: done** (2026-09-15), tagged `v0.2.0`. What was built:

- **Database** (2026-09-14): `modules/postgres` with a traced pool, `DBTX`, `InTx`, error classification, goose migrations under an advisory lock, a readiness check, and `pgtest` against Docker PostgreSQL. Repositories use hand-written SQL ([ADR-0032](adr/0032-repository-sql.md)).
- **Runtime settings** (2026-09-14): `modules/settings` and core `config.Value[T]`: typed declarations, a PostgreSQL store with version checks, history and audit events, `LISTEN/NOTIFY` with periodic resync ([ADR-0031](adr/0031-runtime-settings.md)).
- **Background jobs** (2026-09-14): `modules/jobs` with a River client (context propagation, graceful stop), job definitions with runtime-editable configuration and schedules, a `Manager` for admin APIs, `AsyncSender` and River migrations ([ADR-0033](adr/0033-background-jobs.md)). `aps gen job`, interactive or with flags ([ADR-0035](adr/0035-interactive-cli.md)).
- **Golden app**: `examples/full-single` wiring PostgreSQL, settings and jobs, with `cmd/migrate`, `compose.yaml`, and `/ops/settings`, `/ops/jobs/*` and `/ops/queues`, first behind an interim `OPS_TOKEN`.
- **Audit log**: `modules/auditpg`, an append-only `audit_events` table with `Record` and transactional `RecordTx`, metadata redaction and size bounds, filtered and paginated `List`, and `GET /ops/audit`, `/ops/audit/{id}` (moved from v0.5) ([ADR-0036](adr/0036-audit-storage.md)).
- **Email**: `modules/mail/smtp` and `modules/mail/resend`, core `mail.ErrRejected` and `mail.WithDefaults`, rejected sends cancelled by the mail worker, Mailpit in development, the sender as runtime settings, `GET /ops/mail` and `POST /ops/mail/test`, and `aps add mail` to choose or switch Resend or SMTP ([ADR-0037](adr/0037-email-setup-and-delivery.md)).
- **Authentication** (2026-09-15, [ADR-0038](adr/0038-authentication-v0-2.md)): `modules/auth` building blocks (argon2id, tokens and codes stored as hashes, session middleware and cookies, permission catalog, emails) and an app-owned `internal/modules/auth` with all four layers and `/v1/auth`: register, email codes, login with cookie or bearer token, logout and logout-all, sessions, password reset and change, account deletion, platform roles (`platform_admin`, `ops_viewer`) granted with `go run ./cmd/api grant-role`, and the `auth_cleanup` job. `OPS_TOKEN` was removed: `/ops/*` uses sessions and roles.
- **Example resource** (2026-09-15, [ADR-0039](adr/0039-resource-module-template.md)): `internal/modules/projects`, owned by the signed-in user, with its own table, keyset pagination through `page`, versioned `PATCH`, audit events, cross-owner isolation tests and an end-to-end test. It's the template for `aps gen resource`, which supports string, text and enum fields with unique and filter options, reproduces the module byte for byte, and is tested by generating other resources into a copy of the app.
- **Release tracking** (2026-09-15, [ADR-0040](adr/0040-release-tracking.md)): `modules/releases` records each instance's build with a heartbeat and clean-stop marking, derives releases and prunes old instances; `GET /ops/releases`, `/ops/releases/current` and `/ops/releases/instances` (moved from v0.5).
- **`aps new --preset=full`** (2026-09-15, [ADR-0041](adr/0041-full-preset-generation.md)): templates generated from `examples/full-single` and checked byte for byte, `go.mod` derived from each golden app's, a leak check for repository paths, the database named after the app. A new Full app passes its own tests and accepts `aps gen resource` and `aps gen job`.
- **`aps gen migration`** (2026-09-15): an empty forward-only migration that sorts last, tested by adding a column to a generated resource's table in a new app.
- **Seed data** (2026-09-15, [ADR-0042](adr/0042-development-seed-data.md)): `cmd/seed` creates `admin@example.com` with `platform_admin`, a random password printed once, and three example projects, through the modules' use cases; development only and safe to rerun.
- **`aps dev` with Docker** (2026-09-15, [ADR-0028](adr/0028-local-development-environment.md)): `.env` from `.env.example`, Docker and port checks naming the `.env` line to change, `docker compose up -d --wait`, migrations (again when one changes) and seed data before the app starts; `--no-services`; `--observability` starts Grafana and points the app's OpenTelemetry export at it.
- **Moved out:** the Custom preset to v0.5 (later dropped), the tenancy prompt to v0.4.

| | |
|---|---|
| **Delivers** | `modules/postgres`, `modules/settings` (runtime settings declared in code, stored in PostgreSQL, live on every instance, `/ops/settings` API, `config.Value[T]` in core; [ADR-0031](adr/0031-runtime-settings.md)), `modules/jobs` (River, AsyncSender, job definitions with runtime-editable schedule, enabled, timeout and retries; `/ops/jobs` admin APIs; [ADR-0033](adr/0033-background-jobs.md)), `modules/mail/resend` and `mail/smtp`, `modules/auditpg`, `modules/auth` (email/password, email codes, sessions, logout-all, reset/change password, active sessions, delete account, platform roles, permission catalog), `modules/releases`, Full preset with single-tenant generation, `aps dev` with Docker (PostgreSQL, Mailpit, migrations and seed, `--observability` Grafana), `aps gen resource`, `aps gen job` and `aps gen migration`, seed data, `examples/full-single` |
| **Not included** | Custom preset (v0.5), tenancy prompt (v0.4), social login, 2FA, passkeys, multi-tenant, ops APIs other than `/ops/settings`, `/ops/jobs/*`, `/ops/queues`, `/ops/audit`, `/ops/releases` and `/ops/mail` (audit stats and retention stay in v0.5) |
| **Done when** | From a fresh `aps new --preset=full`: register → verify email code → login → role-protected endpoint → audit event recorded, all in e2e tests; a setting changed through `/ops/settings` on one instance is served by a second instance without restart and appears in history and the audit log; a job generated with `aps gen job` can be rescheduled, disabled and run now through `/ops/jobs` without a restart; nullable `org_id` and role-scope columns present in library tables; threat model rows 12, 13, 19, 23 and 24 addressed |
| **Results** | Covered by tests against Docker PostgreSQL and Mailpit: a new Full app (`APS_E2E=1`) passes its own suite, including sign-up, email verification, login, role-protected `/ops/*` endpoints with audit events, the projects resource, seed data, and a setting changed on one instance served by a second with history and an audit event; the generated `heartbeat` job is rescheduled, run and disabled through `/ops/jobs` without a restart (`TestJobsThroughOps`); nullable `org_id` columns exist in the settings, audit and auth tables; threat model rows reviewed against the code ([ADR-0029](adr/0029-threat-model.md)). `aps dev` in a new Full app (`APS_E2E_DOCKER=1`): API ready 9.6 s after start with images and Go caches warm; a cold run with image pulls isn't measured |

## v0.3: Strong authentication

**Status: done** (2026-09-15), tagged `v0.3.0`. Threat model rows 14–16 reviewed and marked done; every method is covered by its integration tests. What was built:

- **Two-factor authentication** ([ADR-0043](adr/0043-two-factor-authentication.md)): TOTP (RFC 6238) in `modules/auth` with an AES-256-GCM `Keyring` from `AUTH_ENCRYPTION_KEYS` and `rotate-auth-keys`; 10 single-use recovery codes; a sign-in challenge (`POST /v1/auth/login` returns 202, `POST /v1/auth/login/mfa` finishes); replay protection across instances; `Catalog.RequireMFA` and core `actor.Require` with step-up permissions; `platform_admin` and `ops_viewer` require 2FA in every environment (403 `mfa_required`); `reset-mfa` for lost devices; seed data that enrolls the administrator; `aps dev` writing a development key to `.env`.
- **Passkeys** ([ADR-0044](adr/0044-passkeys.md)): `modules/auth/passkey` wrapping `go-webauthn`, with a `passkeytest` software authenticator; passwordless sign-in and passkeys as a second factor; up to 10 per account; single-use ceremonies, user verification and clone detection; `WEBAUTHN_*` configuration with localhost in development; `/.well-known/apple-app-site-association` and `assetlinks.json` for iOS and Android apps; a QR code image in authenticator app setup. After review: changes to sign-in methods need the password once a session's second factor is 10 minutes old, and a passkey can confirm account deletion, turning off the authenticator app and replacing recovery codes.
- **Provider setup** ([ADR-0045](adr/0045-sign-in-provider-setup.md)): `AUTH_PROVIDERS.md` in every Full app, a `.env.example` block per method, a **Sign-in methods** block at start, `go run ./cmd/api auth-providers` and `GET /ops/auth/providers`.
- **Google and Apple sign-in** ([ADR-0046](adr/0046-google-and-apple-sign-in.md)): `modules/auth/social` on `x/oauth2` and `go-oidc`, with a `socialtest` fake provider; an API-hosted web flow with state bound to a cookie, PKCE and nonce; native ID-token sign-in with server nonces; automatic linking on a verified email, removing an unverified account's password; the second factor still required; identities, Apple token revocation and notifications; `APP_PUBLIC_URL`.
- **CLI look** ([ADR-0035](adr/0035-interactive-cli.md) v0.3 notes): prompts in the apistock theme ([theme](brand/theme.md)); `aps new` asks one question at a time, folds each answer into one line, and prints a log of finished steps ending with `next:`.
- **Follow-ups outside the definition of done:** sign-in against real Google and Apple accounts (needs the maintainer's credentials), and Apple token revocation with retries.

| | |
|---|---|
| **Delivers** | Google and Apple sign-in (web and native), account linking, TOTP with recovery codes, passkeys, 2FA policy per role, `AUTH_PROVIDERS.md` |
| **Not included** | GitHub login, API keys |
| **Done when** | Each method passes its integration suite; threat model rows 14–16 reviewed and documented |

## v0.4: Organisations

**Status: done** (2026-09-15), tagged `v0.4.0`; done-when checked end to end and the branch reviewed. Design accepted in [ADR-0048](adr/0048-organisations-v0-4.md). What was built:

- `modules/orgs`: organisation IDs, `RequireMember`, invitation emails; account hooks in the auth module.
- `examples/full-multi` with an app-owned orgs module: organisations, one role per member, invitations for the invited verified address only, personal workspaces, soft delete, restore and the `orgs_purge` job; org-scoped projects with cross-organisation denial tests; a drift check against `full-single`.
- `aps new --tenancy multi`, and `aps gen resource --scope org` (the default in multi-tenant apps), reproducing `full-multi`'s projects module.
- Threat model rows 17, 25 and 26 done ([ADR-0029](adr/0029-threat-model.md)).
- Review fixes: the purge job deletes an organisation only while its purge time has passed, so one restored and deleted again meanwhile stays; `RequireMember` hides organisations when a `Memberships` implementation wraps `ErrNotMember`.
- Moved out: `aps add orgs` to v0.5, because it needs the per-feature recipes and 3-way merges that `aps add` and `aps upgrade` bring.

| | |
|---|---|
| **Delivers** | `modules/orgs` (personal workspaces, memberships, invitations, org roles, ownership transfer, soft delete), multi-tenant generation, the tenancy prompt and `--tenancy` flag in `aps new` (moved from v0.2: before organisations its only answer is single-tenant), `resource/org` template (`aps gen resource --scope org`), `examples/full-multi` with a drift check against `full-single` |
| **Not included** | Row-level security, subdomain tenants, per-org billing |
| **Done when** | Generated cross-org denial tests pass for every org-scoped resource; four isolation layers verified |
| **Results** | Covered by tests against Docker PostgreSQL and Mailpit. Tests: every org-scoped resource gets `TestOrganisationsCantReachEachOthers<Resources>` (read, update, delete and list from another organisation) from `aps gen resource --scope org`; CI generates two more org-scoped resources into a copy of `full-multi` and runs their tests, and a new multi-tenant app (`APS_E2E=1`) generates one and passes its suite. HTTP: routes under `/v1/orgs/{orgId}` answer 404 `org_not_found` to non-members and 404 for another organisation's row (`TestProjectsEndToEnd`). Code: every use case calls `orgs.RequireMember` first and every repository query filters on `org_id` (reviewed; `TestRequiresMembership`). Database: `org_id NOT NULL` with `ON DELETE CASCADE`, `UNIQUE (org_id, id)` and per-organisation uniqueness; purging an organisation removes its rows (`TestPurgingAnOrganisationDeletesItsProjects`). `aps dev` in a new multi-tenant app (`APS_E2E_DOCKER=1`): API ready 11.5 s after start, with the administrator's personal workspace and three example projects |

## v0.5: Operations and upgrades

**Status: done** ([ADR-0050](adr/0050-upgrades-and-adding-features.md), 2026-09-15, tagged `v0.5.0`; `v0.2.0`, `v0.3.0` and `v0.4.0` tagged at their milestone commits). Done: `apistock.lock` v2, written by `aps new` and kept current by `aps add mail`; `aps upgrade` (merge base rebuilt from a checkout tag or the module proxy and proven against the lock, 3-way merges on branch `aps-upgrade/<version>`, `go.mod`, build, `api/openapi.json` and commit), with an end-to-end test upgrading an app created and edited with `aps` at `v0.4.0`; `aps add orgs` (the same merge into the multi-tenant tree, plus migrations that add organisations, give every account a personal workspace and move projects into it, proven on a PostgreSQL database with data). Operations ([ADR-0051](adr/0051-operations-v0-5.md), accepted 2026-09-15): `GET /ops/system`, `GET /ops/audit/stats`, `GET /ops/jobs/overview`, retention as runtime settings with a daily `retention` job and `GET /ops/retention` (audit events kept 365 days by default), maintenance mode keeping sign-in, `/ops`, health checks and docs open with a break-glass command, `api/postman_collection.json` and `api/llms.txt` exported with the spec, and `aps doctor`, which checks the database through the app. What existing apps must do: [upgrade notes](guides/upgrade-notes.md). The Custom preset moved out of v0.5.

| | |
|---|---|
| **Delivers** | `/ops/*` (audit stats, system health, jobs overview, retention), maintenance mode, Postman collection, `llms.txt`, `aps upgrade` (3-way merge on a branch), `aps add orgs` (single → multi-tenant, moved from v0.4, [ADR-0048](adr/0048-organisations-v0-4.md)), `aps doctor`, `apistock.lock` v2 (release, template inputs and file hashes) |
| **Not included** | Custom preset (moved out by [ADR-0050](adr/0050-upgrades-and-adding-features.md): each combination needs its own golden tree, and today's real choices are tenancy and the email provider), feature flags, live observability, incidents |
| **Done when** | A Full app generated with `aps` at `v0.4.0` and edited by script (a changed line in a tracked file, a new resource, a new migration, `aps add mail smtp`) upgrades to v0.5 in CI with every edit kept, builds and passes its tests; the same app converted with `aps add orgs` has the schema of a new multi-tenant app and passes its suite; ops endpoints require platform roles and 2FA |
| **Results** | Checked locally with Docker PostgreSQL (GitHub CI is paused). Upgrade (`APS_E2E=1 TestUpgradeFromV040`): a Full app created by `aps` built at `v0.4.0`, with an edited `routes.go`, a generated resource and migration, and `aps add mail --provider smtp`, upgrades with every edit kept, the template's `routes.go` change merged in beside the edit, a v2 lock recording SMTP, the API files regenerated and the upgrade committed; the app builds, and no test that passed before fails after. Three of the app's own tests already fail after `aps add mail --provider smtp` at `v0.4.0` (they assume Resend); that is fixed separately. Organisations (`TestAddOrgsConvertsADatabase`): a single-tenant database with data converts to a new multi-tenant app's columns, constraints and indexes, and the converted app passes its suite. Ops protection: `TestEveryOperationAuthorizesFirst` calls every ops use case without a session, without permissions and needing 2FA; `TestOpsOperationsDeclareSecurity` checks every `/ops` operation's contract. Threat model rows 17, 18 and 27–31 done ([ADR-0029](adr/0029-threat-model.md)) |

## v1.0: Stable

**Status: planned.**

| | |
|---|---|
| **Delivers** | External security review with findings fixed, API freeze and stability tiers in force, documentation content ready for the public site ([ADR-0049](adr/0049-public-docs-and-website.md)), domain hardening complete (including rate limits shared across instances with trusted-proxy client IPs, done in [ADR-0052](adr/0052-shared-rate-limits.md)), governance and contribution guide |
| **Done when** | Security review signed off; `gorelease` baseline recorded; scaffold compatibility promise ([ADR-0016](adr/0016-scaffold-compatibility-and-upgrades.md)) active |

## v1.1

**Status: planned.**

- Feature flags and per-org settings
- Live observability and incident reports
- API keys and service accounts
- GitHub login
- PostgreSQL row-level security option
- Idempotency keys
- Custom local dev console
- Resend bounce and complaint webhooks
- Prometheus `/metrics` option

## v1.2: Client templates

**Status: proposed** ([ADR-0047](adr/0047-client-templates.md)).

| | |
|---|---|
| **Delivers** | `aps new` asks for a docs site, a dashboard and mobile apps (`--docs`, `--dashboard`, `--mobile none\|expo\|native`) and creates each from its own template repository, with the app name, bundle ID, API URL and sign-in methods filled in. Templates are pinned, hashed and signed archives listed in each `aps` release, cached locally, with `--templates <path>` for local checkouts. Each template declares what it accepts in `apistock-template.json` and calls the API through clients generated from `api/openapi.json`. Order: fetch and fill-in mechanism with `template-docs`, then `template-dashboard`, then `template-expo` (iOS and Android) |
| **Not included** | Native iOS and Android templates (only when builders ask for them), hybrid apps, upgrading client code with `aps upgrade` (client output is one-shot) |
| **Done when** | CI creates an app with every template against `examples/full-single` and builds each part; a tampered archive or unlisted version is refused; sign-in, 2FA and passkeys work from the dashboard and the Expo app; threat model rows for templates added |

## v1.3: Public website

**Status: in progress, built ahead of order** ([ADR-0049](adr/0049-public-docs-and-website.md), accepted 2026-09-15). What's done:

- The website: `apistock.dev` (landing page) and `docs.apistock.dev` are a separate Next.js repository, apistock-web, deployed on Vercel. Its docs app renders a snapshot of this repository's `docs/` (plus the golden app's `ARCHITECTURE.md` and `openapi.json`), synced with `pnpm sync-docs` whenever the docs here change; `docs/docs.json` lists the tabs and pages. The Go generator that first built both sites in `site/` was removed on 2026-09-15.
- Docs in two audiences: **Guides** for app builders (prerequisites, a quickstart verified against a real run, first resource, every sign-in credential step by step, go-live, troubleshooting) and **Technical** documentation (architecture, key decisions, request lifecycle, app internals, services and libraries, environment variables, secrets and keys, subsystems, error handling, testing, production), plus a package reference generated from Go doc comments, the CLI reference, decision records and this roadmap.
- An API reference with request and response examples and "Try it", rendered from `examples/full-multi/api/openapi.json`.
- Search, light and dark themes, Markdown copies of every page, `llms.txt`, and redirects for moved pages.
- The logo kit in `docs/brand/logo`.
- Generated apps' `/docs` in the same design: `modules/openapi/reference` renders every app's own endpoints with examples, "Try it" and search under a strict Content-Security-Policy with embedded fonts, replacing the embedded Scalar.

Still to do: versioned docs, and compiling the site's code snippets.

| | |
|---|---|
| **Delivers** | `apistock.dev` landing page; `docs.apistock.dev` framework documentation (guides, modules, CLI, decision records, changelog) with search, a version per minor release, copy as Markdown and `llms.txt`; a public API reference at `docs.apistock.dev/api-reference` rendered from `examples/full-multi/api/openapi.json`; all three in the apistock look ([theme](brand/theme.md)) with Mintlify's page structure; generated apps' `/docs` restyled with the same system in the app's own name and accent. Built by the apistock-web Next.js repository from `docs/` and deployed on Vercel |
| **Not included** | Hosting docs for developers' own apps, a blog, community module index pages (Later), translations |
| **Done when** | CI builds the site and fails on broken links, WCAG 2.2 AA failures in both themes and code snippets that no longer compile or match the golden apps; the public API reference and a new app's `/docs` render the same `openapi.json` with the same theme; the landing page and a docs page reach Largest Contentful Paint within 2.5 s on a mid-range phone over 4G |

## Later

- Native iOS (SwiftUI) and Android (Kotlin) client templates
- Community module index and author tooling
- Subdomain tenant resolution
- Per-org quotas and billing
- Storage and outgoing webhooks modules
- Enterprise SSO integration
- WASI-sandboxed generators

## Not planned

- An admin web UI inside generated apps (a dashboard comes as a client template instead)
- A hosted control plane
- Hybrid mobile apps
- Databases other than PostgreSQL
- Schema- or database-per-tenant
- A custom router, ORM or DI container
- A plugin runtime
- Secrets or infrastructure configuration stored in the database
- Application logs stored in PostgreSQL
