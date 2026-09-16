# gorbital Roadmap

**Status:** Accepted (2026-09-14) · **Replaces:** `scope-v1.md`

gorbital ships through pre-release milestones. Each one is usable on its own and has a definition of done. Nothing outside a milestone's scope starts without an accepted ADR. Everything is v0 until 1.0 ([ADR-0015](adr/0015-public-api-and-stability-tiers.md)).

## Where things stand

| Milestone | Theme | Status | What you can use today |
|---|---|---|---|
| [v0.1](#v01-foundation) | Foundation | ✅ Done | `orb new` (Minimal), `orb dev`, core library, `/docs` |
| [v0.2](#v02-data-and-identity) | Data and identity | ✅ Done, tagged `v0.2.0` | Full preset: PostgreSQL, settings, jobs, email, sign-in, roles, audit, `orb gen` |
| [v0.3](#v03-strong-authentication) | Strong authentication | ✅ Done, tagged `v0.3.0` | Authenticator apps, passkeys, Google and Apple sign-in |
| [v0.4](#v04-organisations) | Organisations | ✅ Done, tagged `v0.4.0` | `orb new --tenancy multi`, org-scoped resources |
| [v0.5](#v05-operations-and-upgrades) | Operations and upgrades | ✅ Done, tagged `v0.5.0` | `orb upgrade`, `orb add orgs`, `orb doctor`, ops endpoints, maintenance mode |
| [v1.0](#v10-stable) | Stable | 🔨 In progress | Rate limits shared across instances, trusted proxies |
| [v1.1](#v11-operations-and-integrations) | Operations and integrations | Planned (2026-09-16) | |
| [v1.2](#v12-client-templates) | Client templates | Proposed | |
| [v1.3](#v13-public-website) | Public website | 🔨 Built early, published | gorbital.dev, docs.gorbital.dev |

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
| **Delivers** | Core packages (`app`, `httpx`, `health`, `actor`, `requestid`, `audit`, `mail`, `config`, `page`, `ratelimit`, `buildinfo`), `modules/openapi` (Huma, problem errors, embedded Scalar, since replaced by the gorbital reference in v1.3), `modules/telemetry` (OpenTelemetry, correlated logs), hand-written `examples/minimal`, Minimal recipe generated from it, `orb new` (Minimal), `orb dev` (reload, `.env`, port check), `orb version`, security defaults, Dockerfile, project CI (tests on Go 1.26/1.27, race, golangci-lint, govulncheck, gitleaks, dependency budget, template and OpenAPI drift, end-to-end), signed release workflow for `orb` |
| **Results** | First run with the real CLI (`scripts/first-run.sh`): **25.0 s** from clean caches, **4.8 s** warm. Generated app passes its own tests. Lint: 0 issues in all modules. `gorelease` starts at the first tag. |
| **Not included** | Database, auth, Full/Custom presets, Docker services |
| **Done when** | On a clean machine: `go install` → `orb new my-api` → `orb dev` → `/docs` in under 60 seconds; generator reproduces `examples/minimal` exactly; threat model rows 2–10 and 21 addressed |

## v0.2: Data and identity

**Status: done** (2026-09-15), tagged `v0.2.0`. What was built:

- **Database** (2026-09-14): `modules/postgres` with a traced pool, `DBTX`, `InTx`, error classification, goose migrations under an advisory lock, a readiness check, and `pgtest` against Docker PostgreSQL. Repositories use hand-written SQL ([ADR-0032](adr/0032-repository-sql.md)).
- **Runtime settings** (2026-09-14): `modules/settings` and core `config.Value[T]`: typed declarations, a PostgreSQL store with version checks, history and audit events, `LISTEN/NOTIFY` with periodic resync ([ADR-0031](adr/0031-runtime-settings.md)).
- **Background jobs** (2026-09-14): `modules/jobs` with a River client (context propagation, graceful stop), job definitions with runtime-editable configuration and schedules, a `Manager` for admin APIs, `AsyncSender` and River migrations ([ADR-0033](adr/0033-background-jobs.md)). `orb gen job`, interactive or with flags ([ADR-0035](adr/0035-interactive-cli.md)).
- **Golden app**: `examples/full-single` wiring PostgreSQL, settings and jobs, with `cmd/migrate`, `compose.yaml`, and `/ops/settings`, `/ops/jobs/*` and `/ops/queues`, first behind an interim `OPS_TOKEN`.
- **Audit log**: `modules/auditpg`, an append-only `audit_events` table with `Record` and transactional `RecordTx`, metadata redaction and size bounds, filtered and paginated `List`, and `GET /ops/audit`, `/ops/audit/{id}` (moved from v0.5) ([ADR-0036](adr/0036-audit-storage.md)).
- **Email**: `modules/mail/smtp` and `modules/mail/resend`, core `mail.ErrRejected` and `mail.WithDefaults`, rejected sends cancelled by the mail worker, Mailpit in development, the sender as runtime settings, `GET /ops/mail` and `POST /ops/mail/test`, and `orb add mail` to choose or switch Resend or SMTP ([ADR-0037](adr/0037-email-setup-and-delivery.md)).
- **Authentication** (2026-09-15, [ADR-0038](adr/0038-authentication-v0-2.md)): `modules/auth` building blocks (argon2id, tokens and codes stored as hashes, session middleware and cookies, permission catalog, emails) and an app-owned `internal/modules/auth` with all four layers and `/v1/auth`: register, email codes, login with cookie or bearer token, logout and logout-all, sessions, password reset and change, account deletion, platform roles (`platform_admin`, `ops_viewer`) granted with `go run ./cmd/api grant-role`, and the `auth_cleanup` job. `OPS_TOKEN` was removed: `/ops/*` uses sessions and roles.
- **Example resource** (2026-09-15, [ADR-0039](adr/0039-resource-module-template.md)): `internal/modules/projects`, owned by the signed-in user, with its own table, keyset pagination through `page`, versioned `PATCH`, audit events, cross-owner isolation tests and an end-to-end test. It's the template for `orb gen resource`, which supports string, text and enum fields with unique and filter options, reproduces the module byte for byte, and is tested by generating other resources into a copy of the app.
- **Release tracking** (2026-09-15, [ADR-0040](adr/0040-release-tracking.md)): `modules/releases` records each instance's build with a heartbeat and clean-stop marking, derives releases and prunes old instances; `GET /ops/releases`, `/ops/releases/current` and `/ops/releases/instances` (moved from v0.5).
- **`orb new --preset=full`** (2026-09-15, [ADR-0041](adr/0041-full-preset-generation.md)): templates generated from `examples/full-single` and checked byte for byte, `go.mod` derived from each golden app's, a leak check for repository paths, the database named after the app. A new Full app passes its own tests and accepts `orb gen resource` and `orb gen job`.
- **`orb gen migration`** (2026-09-15): an empty forward-only migration that sorts last, tested by adding a column to a generated resource's table in a new app.
- **Seed data** (2026-09-15, [ADR-0042](adr/0042-development-seed-data.md)): `cmd/seed` creates `admin@example.com` with `platform_admin`, a random password printed once, and three example projects, through the modules' use cases; development only and safe to rerun.
- **`orb dev` with Docker** (2026-09-15, [ADR-0028](adr/0028-local-development-environment.md)): `.env` from `.env.example`, Docker and port checks naming the `.env` line to change, `docker compose up -d --wait`, migrations (again when one changes) and seed data before the app starts; `--no-services`; `--observability` starts Grafana and points the app's OpenTelemetry export at it.
- **Moved out:** the Custom preset to v0.5 (later dropped), the tenancy prompt to v0.4.

| | |
|---|---|
| **Delivers** | `modules/postgres`, `modules/settings` (runtime settings declared in code, stored in PostgreSQL, live on every instance, `/ops/settings` API, `config.Value[T]` in core; [ADR-0031](adr/0031-runtime-settings.md)), `modules/jobs` (River, AsyncSender, job definitions with runtime-editable schedule, enabled, timeout and retries; `/ops/jobs` admin APIs; [ADR-0033](adr/0033-background-jobs.md)), `modules/mail/resend` and `mail/smtp`, `modules/auditpg`, `modules/auth` (email/password, email codes, sessions, logout-all, reset/change password, active sessions, delete account, platform roles, permission catalog), `modules/releases`, Full preset with single-tenant generation, `orb dev` with Docker (PostgreSQL, Mailpit, migrations and seed, `--observability` Grafana), `orb gen resource`, `orb gen job` and `orb gen migration`, seed data, `examples/full-single` |
| **Not included** | Custom preset (v0.5), tenancy prompt (v0.4), social login, 2FA, passkeys, multi-tenant, ops APIs other than `/ops/settings`, `/ops/jobs/*`, `/ops/queues`, `/ops/audit`, `/ops/releases` and `/ops/mail` (audit stats and retention stay in v0.5) |
| **Done when** | From a fresh `orb new --preset=full`: register → verify email code → login → role-protected endpoint → audit event recorded, all in e2e tests; a setting changed through `/ops/settings` on one instance is served by a second instance without restart and appears in history and the audit log; a job generated with `orb gen job` can be rescheduled, disabled and run now through `/ops/jobs` without a restart; nullable `org_id` and role-scope columns present in library tables; threat model rows 12, 13, 19, 23 and 24 addressed |
| **Results** | Covered by tests against Docker PostgreSQL and Mailpit: a new Full app (`ORB_E2E=1`) passes its own suite, including sign-up, email verification, login, role-protected `/ops/*` endpoints with audit events, the projects resource, seed data, and a setting changed on one instance served by a second with history and an audit event; the generated `heartbeat` job is rescheduled, run and disabled through `/ops/jobs` without a restart (`TestJobsThroughOps`); nullable `org_id` columns exist in the settings, audit and auth tables; threat model rows reviewed against the code ([ADR-0029](adr/0029-threat-model.md)). `orb dev` in a new Full app (`ORB_E2E_DOCKER=1`): API ready 9.6 s after start with images and Go caches warm; a cold run with image pulls isn't measured |

## v0.3: Strong authentication

**Status: done** (2026-09-15), tagged `v0.3.0`. Threat model rows 14–16 reviewed and marked done; every method is covered by its integration tests. What was built:

- **Two-factor authentication** ([ADR-0043](adr/0043-two-factor-authentication.md)): TOTP (RFC 6238) in `modules/auth` with an AES-256-GCM `Keyring` from `AUTH_ENCRYPTION_KEYS` and `rotate-auth-keys`; 10 single-use recovery codes; a sign-in challenge (`POST /v1/auth/login` returns 202, `POST /v1/auth/login/mfa` finishes); replay protection across instances; `Catalog.RequireMFA` and core `actor.Require` with step-up permissions; `platform_admin` and `ops_viewer` require 2FA in every environment (403 `mfa_required`); `reset-mfa` for lost devices; seed data that enrolls the administrator; `orb dev` writing a development key to `.env`.
- **Passkeys** ([ADR-0044](adr/0044-passkeys.md)): `modules/auth/passkey` wrapping `go-webauthn`, with a `passkeytest` software authenticator; passwordless sign-in and passkeys as a second factor; up to 10 per account; single-use ceremonies, user verification and clone detection; `WEBAUTHN_*` configuration with localhost in development; `/.well-known/apple-app-site-association` and `assetlinks.json` for iOS and Android apps; a QR code image in authenticator app setup. After review: changes to sign-in methods need the password once a session's second factor is 10 minutes old, and a passkey can confirm account deletion, turning off the authenticator app and replacing recovery codes.
- **Provider setup** ([ADR-0045](adr/0045-sign-in-provider-setup.md)): `AUTH_PROVIDERS.md` in every Full app, a `.env.example` block per method, a **Sign-in methods** block at start, `go run ./cmd/api auth-providers` and `GET /ops/auth/providers`.
- **Google and Apple sign-in** ([ADR-0046](adr/0046-google-and-apple-sign-in.md)): `modules/auth/social` on `x/oauth2` and `go-oidc`, with a `socialtest` fake provider; an API-hosted web flow with state bound to a cookie, PKCE and nonce; native ID-token sign-in with server nonces; automatic linking on a verified email, removing an unverified account's password; the second factor still required; identities, Apple token revocation and notifications; `APP_PUBLIC_URL`.
- **CLI look** ([ADR-0035](adr/0035-interactive-cli.md) v0.3 notes): prompts in the gorbital theme ([theme](brand/theme.md)); `orb new` asks one question at a time, folds each answer into one line, and prints a log of finished steps ending with `next:`.
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
- `orb new --tenancy multi`, and `orb gen resource --scope org` (the default in multi-tenant apps), reproducing `full-multi`'s projects module.
- Threat model rows 17, 25 and 26 done ([ADR-0029](adr/0029-threat-model.md)).
- Review fixes: the purge job deletes an organisation only while its purge time has passed, so one restored and deleted again meanwhile stays; `RequireMember` hides organisations when a `Memberships` implementation wraps `ErrNotMember`.
- Moved out: `orb add orgs` to v0.5, because it needs the per-feature recipes and 3-way merges that `orb add` and `orb upgrade` bring.

| | |
|---|---|
| **Delivers** | `modules/orgs` (personal workspaces, memberships, invitations, org roles, ownership transfer, soft delete), multi-tenant generation, the tenancy prompt and `--tenancy` flag in `orb new` (moved from v0.2: before organisations its only answer is single-tenant), `resource/org` template (`orb gen resource --scope org`), `examples/full-multi` with a drift check against `full-single` |
| **Not included** | Row-level security, subdomain tenants, per-org billing |
| **Done when** | Generated cross-org denial tests pass for every org-scoped resource; four isolation layers verified |
| **Results** | Covered by tests against Docker PostgreSQL and Mailpit. Tests: every org-scoped resource gets `TestOrganisationsCantReachEachOthers<Resources>` (read, update, delete and list from another organisation) from `orb gen resource --scope org`; CI generates two more org-scoped resources into a copy of `full-multi` and runs their tests, and a new multi-tenant app (`ORB_E2E=1`) generates one and passes its suite. HTTP: routes under `/v1/orgs/{orgId}` answer 404 `org_not_found` to non-members and 404 for another organisation's row (`TestProjectsEndToEnd`). Code: every use case calls `orgs.RequireMember` first and every repository query filters on `org_id` (reviewed; `TestRequiresMembership`). Database: `org_id NOT NULL` with `ON DELETE CASCADE`, `UNIQUE (org_id, id)` and per-organisation uniqueness; purging an organisation removes its rows (`TestPurgingAnOrganisationDeletesItsProjects`). `orb dev` in a new multi-tenant app (`ORB_E2E_DOCKER=1`): API ready 11.5 s after start, with the administrator's personal workspace and three example projects |

## v0.5: Operations and upgrades

**Status: done** ([ADR-0050](adr/0050-upgrades-and-adding-features.md), 2026-09-15, tagged `v0.5.0`; `v0.2.0`, `v0.3.0` and `v0.4.0` tagged at their milestone commits). Done: `gorbital.lock` v2, written by `orb new` and kept current by `orb add mail`; `orb upgrade` (merge base rebuilt from a checkout tag or the module proxy and proven against the lock, 3-way merges on branch `orb-upgrade/<version>`, `go.mod`, build, `api/openapi.json` and commit), with an end-to-end test upgrading an app created and edited with `orb` at `v0.4.0`; `orb add orgs` (the same merge into the multi-tenant tree, plus migrations that add organisations, give every account a personal workspace and move projects into it, proven on a PostgreSQL database with data). Operations ([ADR-0051](adr/0051-operations-v0-5.md), accepted 2026-09-15): `GET /ops/system`, `GET /ops/audit/stats`, `GET /ops/jobs/overview`, retention as runtime settings with a daily `retention` job and `GET /ops/retention` (audit events kept 365 days by default), maintenance mode keeping sign-in, `/ops`, health checks and docs open with a break-glass command, `api/postman_collection.json` and `api/llms.txt` exported with the spec, and `orb doctor`, which checks the database through the app. What existing apps must do: [upgrade notes](guides/upgrade-notes.md). The Custom preset moved out of v0.5.

| | |
|---|---|
| **Delivers** | `/ops/*` (audit stats, system health, jobs overview, retention), maintenance mode, Postman collection, `llms.txt`, `orb upgrade` (3-way merge on a branch), `orb add orgs` (single → multi-tenant, moved from v0.4, [ADR-0048](adr/0048-organisations-v0-4.md)), `orb doctor`, `gorbital.lock` v2 (release, template inputs and file hashes) |
| **Not included** | Custom preset (moved out by [ADR-0050](adr/0050-upgrades-and-adding-features.md): each combination needs its own golden tree, and today's real choices are tenancy and the email provider), feature flags, live observability, incidents |
| **Done when** | A Full app generated with `orb` at `v0.4.0` and edited by script (a changed line in a tracked file, a new resource, a new migration, `orb add mail smtp`) upgrades to v0.5 in CI with every edit kept, builds and passes its tests; the same app converted with `orb add orgs` has the schema of a new multi-tenant app and passes its suite; ops endpoints require platform roles and 2FA |
| **Results** | Checked locally with Docker PostgreSQL (GitHub CI is paused). Upgrade (`ORB_E2E=1 TestUpgradeFromV040`): a Full app created by `orb` built at `v0.4.0`, with an edited `routes.go`, a generated resource and migration, and `orb add mail --provider smtp`, upgrades with every edit kept, the template's `routes.go` change merged in beside the edit, a v2 lock recording SMTP, the API files regenerated and the upgrade committed; the app builds, and no test that passed before fails after. Three of the app's own tests already fail after `orb add mail --provider smtp` at `v0.4.0` (they assume Resend); that is fixed separately. Organisations (`TestAddOrgsConvertsADatabase`): a single-tenant database with data converts to a new multi-tenant app's columns, constraints and indexes, and the converted app passes its suite. Ops protection: `TestEveryOperationAuthorizesFirst` calls every ops use case without a session, without permissions and needing 2FA; `TestOpsOperationsDeclareSecurity` checks every `/ops` operation's contract. Threat model rows 17, 18 and 27–31 done ([ADR-0029](adr/0029-threat-model.md)) |

## v1.0: Stable

**Status: in progress.** Plan accepted by the maintainer on 2026-09-16. Done so far: rate limits shared across instances with trusted-proxy client IPs ([ADR-0052](adr/0052-shared-rate-limits.md)); the API freeze and scaffold compatibility checks ([ADR-0054](adr/0054-api-freeze-and-scaffold-compatibility.md)). The external review needs a third party, so v1.0 is built and reviewed internally first and stays **awaiting external sign-off** until that review is done.

Plan, in order:

| # | Work | Decision record |
|---|---|---|
| 1 | **Security review (internal).** A review of the library, the CLI and generator, and both Full apps, area by area (sessions and sign-in, 2FA and passkeys, social sign-in, organisations, ops and settings, HTTP core, generator and upgrades, supply chain). Every finding fixed or accepted in writing; the report published under `docs/security/`; the threat model's open rows updated. Includes the generator copying a golden app's local `.env` into templates | ADR-0053 |
| 2 | ✅ **API freeze, enforced** (done 2026-09-16). A `Stability:` line in every package doc (checked by `internal/archtest`); committed API listings per Go module in `api/*.txt`, recorded and checked by `internal/tools/apicheck` (missing lines fail as breaking, new lines until recorded with `-write`) in CI; `api/surface.json` in both Full apps and generated apps (error codes, audit actions, permissions and roles, setting keys, job names; `TestPublicSurface` fails on a removal or an unrecorded addition; upgrades record it); `api/openapi.baseline.json` and `TestOpsAPICompatible` on `/ops/*` through the new `openapi.CheckCompatible`; `"schemaVersion": 1` in every `orb … --json` output (and `orb version --json`) with golden files in `cli/internal/cli/testdata/json`; `gorelease` against the previous tag in `release-library.yml` | ADR-0054 |
| 3 | ✅ **Scaffold compatibility promise active** (done 2026-09-16, [ADR-0016](adr/0016-scaffold-compatibility-and-upgrades.md)): `TestScaffoldCompatibility` (`ORB_COMPAT=1`, CI job `compatibility`) builds `orb` at the latest `v1.*` tag, generates Full single- and multi-tenant apps against the current library and builds, vets and tests them; skips until a v1 tag exists; `ORB_COMPAT_FROM` checks other tags (v0.5.0 fails, as v0 may: it predates the rename). `orb upgrade --major` waits for the first v2 bridge release | ADR-0054 |
| 4 | ✅ **Governance and contribution** (done 2026-09-16): `GOVERNANCE.md`, `CONTRIBUTING.md`, `CODEOWNERS`, pull request and issue templates, `SECURITY.md` supported versions, `CHANGELOG.md` | ADR-0055 |
| 5 | **Documentation content ready:** reference pages for error codes, audit actions, permissions, settings and jobs generated from the inventory; a changelog page; stale pages fixed; the docs site resynced | — |

| | |
|---|---|
| **Delivers** | Internal security review with findings fixed, API freeze and stability tiers in force, documentation content ready for the public site ([ADR-0049](adr/0049-public-docs-and-website.md)), domain hardening complete (including rate limits shared across instances with trusted-proxy client IPs, done in [ADR-0052](adr/0052-shared-rate-limits.md)), governance and contribution guide |
| **Not included** | The external review itself (maintainer action), publishing the library at `gorbital.dev` and tags (maintainer action, threat model rows 1 and 8), `orb upgrade --major` |
| **Done when** | Every internal review finding is fixed or accepted in the report; API listings, surface inventories and the `/ops` OpenAPI baseline are recorded and their checks fail on a removal; every `--json` output has `schemaVersion` and a golden test; the compatibility check runs; external security review signed off (open) |

## v1.1: Operations and integrations

**Status: planned** (2026-09-16). APIs only: the Dev Portal and Observability Portal in gorbital-dashboards stay on mock data for now. Each feature lands in the library, both Full golden apps and their templates, with an upgrade note, in this order:

| # | Feature | Shape | Decision record |
|---|---|---|---|
| 1 | **Per-organisation settings** | `modules/settings` uses its reserved `org_id`: settings declared as org-overridable, `Setting.Get` resolving the organisation from the context, `/v1/orgs/{orgId}/settings` for org admins, `/ops/settings` unchanged | ADR-0056 |
| 2 | **Feature flags** | New `modules/flags`: flags declared in code, on/off with per-organisation and per-user targeting and a stable percentage rollout, stored in PostgreSQL and reloaded on every instance, `/ops/flags` with reasons, history and audit events | ADR-0057 |
| 3 | **API keys and service accounts** | Service accounts as non-human principals with roles (platform, or organisation in multi-tenant apps); API keys shown once, stored as hashes, with an expiry, scopes and last use; accepted as bearer tokens by the auth middleware; roles that require 2FA can't be given to a service account | ADR-0058 |
| 4 | **GitHub login** | A non-OpenID provider in `modules/auth/social` (verified primary email from GitHub's API), web flow only, with the same linking rules | ADR-0059 |
| 5 | **Idempotency keys** | New `modules/idempotency`: an `Idempotency-Key` header on POST requests stores the response per caller for 24 hours, replays it, and refuses a different body or a request still in progress | ADR-0060 |
| 6 | **Row-level security option** | Multi-tenant apps set the organisation on every transaction; `orb add rls` adds policies and forces row-level security on org-scoped tables as a fifth isolation layer | ADR-0061 |
| 7 | **Resend bounce and complaint webhooks** | Signed webhook verification in `modules/mail/resend`, a suppression list checked before sending, `/ops/mail/suppressions` | ADR-0062 |
| 8 | **Prometheus `/metrics` option** | `modules/telemetry` Prometheus exporter, served on a separate `METRICS_ADDR` listener, off by default | ADR-0063 |
| 9 | **Live observability and incident reports** | Per-instance request, error and latency windows shared through PostgreSQL, `/ops/observability` (with a live stream), and incidents opened by operators or by error-rate thresholds, with updates, resolution, reports and audit events | ADR-0064 |
| 10 | **Local dev console APIs** | Development-only `/_dev/*` endpoints for the console (routes, wiring, configuration without secrets, captured email, recent requests), bound to localhost, checking the `Host` header and a session token printed by `orb dev` | ADR-0065 |

| | |
|---|---|
| **Not included** | Dashboard user interfaces, billing, GitHub organisation or team sync, other webhook providers, alert delivery (paging, chat) |
| **Done when** | Each feature has its accepted ADR, library tests against Docker PostgreSQL, end-to-end tests in both Full apps (cross-organisation tests where scoped), regenerated templates and API files, an upgrade note and docs; the site is resynced |

## v1.2: Client templates

**Status: proposed** ([ADR-0047](adr/0047-client-templates.md)).

| | |
|---|---|
| **Delivers** | `orb new` asks for a docs site, a dashboard and mobile apps (`--docs`, `--dashboard`, `--mobile none\|expo\|native`) and creates each from its own template repository, with the app name, bundle ID, API URL and sign-in methods filled in. Templates are pinned, hashed and signed archives listed in each `orb` release, cached locally, with `--templates <path>` for local checkouts. Each template declares what it accepts in `gorbital-template.json` and calls the API through clients generated from `api/openapi.json`. Order: fetch and fill-in mechanism with `template-docs`, then `template-dashboard`, then `template-expo` (iOS and Android) |
| **Not included** | Native iOS and Android templates (only when builders ask for them), hybrid apps, upgrading client code with `orb upgrade` (client output is one-shot) |
| **Done when** | CI creates an app with every template against `examples/full-single` and builds each part; a tampered archive or unlisted version is refused; sign-in, 2FA and passkeys work from the dashboard and the Expo app; threat model rows for templates added |

## v1.3: Public website

**Status: in progress, built ahead of order** ([ADR-0049](adr/0049-public-docs-and-website.md), accepted 2026-09-15). What's done:

- The website: `gorbital.dev` (landing page) and `docs.gorbital.dev` are a separate Next.js repository, gorbital-web, deployed on Vercel. Its docs app renders a snapshot of this repository's `docs/` (plus the golden app's `ARCHITECTURE.md` and `openapi.json`), synced with `pnpm sync-docs` whenever the docs here change; `docs/docs.json` lists the tabs and pages. The Go generator that first built both sites in `site/` was removed on 2026-09-15.
- Docs in two audiences: **Guides** for app builders (prerequisites, a quickstart verified against a real run, first resource, every sign-in credential step by step, go-live, troubleshooting) and **Technical** documentation (architecture, key decisions, request lifecycle, app internals, services and libraries, environment variables, secrets and keys, subsystems, error handling, testing, production), plus a package reference generated from Go doc comments, the CLI reference, decision records and this roadmap.
- An API reference with request and response examples and "Try it", rendered from `examples/full-multi/api/openapi.json`.
- Search, light and dark themes, Markdown copies of every page, `llms.txt`, and redirects for moved pages.
- The logo kit in `docs/brand/logo`.
- Generated apps' `/docs` in the same design: `modules/openapi/reference` renders every app's own endpoints with examples, "Try it" and search under a strict Content-Security-Policy with embedded fonts, replacing the embedded Scalar.

Still to do: versioned docs, and compiling the site's code snippets.

| | |
|---|---|
| **Delivers** | `gorbital.dev` landing page; `docs.gorbital.dev` framework documentation (guides, modules, CLI, decision records, changelog) with search, a version per minor release, copy as Markdown and `llms.txt`; a public API reference at `docs.gorbital.dev/api-reference` rendered from `examples/full-multi/api/openapi.json`; all three in the gorbital look ([theme](brand/theme.md)) with Mintlify's page structure; generated apps' `/docs` restyled with the same system in the app's own name and accent. Built by the gorbital-web Next.js repository from `docs/` and deployed on Vercel |
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
