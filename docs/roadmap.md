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

| | |
|---|---|
| **Delivers** | `modules/postgres`, `modules/jobs` (River, cron, AsyncSender), `modules/mail/resend` and `mail/smtp`, `modules/auditpg`, `modules/auth` (email/password, email codes, sessions, logout-all, reset/change password, active sessions, delete account, platform roles, permission catalog), `modules/releases`, Full and Custom presets, tenancy prompt with single-tenant generation, `aps dev` with Docker (PostgreSQL, Mailpit, `--observability` Grafana), `aps gen resource` and `aps gen migration`, seed data, `examples/full-single` |
| **Not included** | Social login, 2FA, passkeys, multi-tenant, ops APIs |
| **Done when** | From a fresh `aps new --preset=full`: register → verify email code → login → role-protected endpoint → audit event recorded, all in e2e tests; nullable `org_id` and role-scope columns present in library tables |

## v0.3: Strong authentication

| | |
|---|---|
| **Delivers** | Google and Apple sign-in (web and native), account linking, TOTP with recovery codes, passkeys, 2FA policy per role, `docs/auth-providers.md` |
| **Not included** | GitHub login, API keys |
| **Done when** | Each method passes its integration suite; threat model rows 14–16 reviewed and documented |

## v0.4: Organisations

| | |
|---|---|
| **Delivers** | `modules/orgs` (personal workspaces, memberships, invitations, org roles, ownership transfer, soft delete), multi-tenant generation, `resource/org` template, `aps add orgs` single → multi path, `examples/full-multi` |
| **Not included** | Row-level security, subdomain tenants, per-org billing |
| **Done when** | Generated cross-org denial tests pass for every org-scoped resource; four isolation layers verified |

## v0.5: Operations and upgrades

| | |
|---|---|
| **Delivers** | `/ops/*` (audit logs, system health, release monitor, jobs overview, retention), maintenance mode, Postman collection, `llms.txt`, `aps upgrade` (3-way merge on a branch), `aps doctor` |
| **Not included** | Configuration center, feature flags, live observability, incidents |
| **Done when** | An app generated with v0.2 and edited by script upgrades to v0.5 in CI with no lost edits; ops endpoints require platform roles and 2FA |

## v1.0: Stable

| | |
|---|---|
| **Delivers** | External security review with findings fixed, API freeze and stability tiers in force, `apistock.dev` docs site (Mintlify), domain hardening complete, governance and contribution guide |
| **Done when** | Security review signed off; `gorelease` baseline recorded; scaffold compatibility promise ([ADR-0016](adr/0016-scaffold-compatibility-and-upgrades.md)) active |

## v1.1

Configuration center, feature flags, live observability, incident reports, API keys and service accounts, GitHub login, PostgreSQL row-level security option, idempotency keys, custom local dev console, Resend bounce/complaint webhooks, Prometheus `/metrics` option.

## Later

Community module index and author tooling, subdomain tenant resolution, per-org quotas and billing, storage and outgoing webhooks modules, enterprise SSO integration, WASI-sandboxed generators.

## Not planned

Admin web UI inside generated apps, hosted control plane, mobile app, databases other than PostgreSQL, schema- or database-per-tenant, custom router, ORM or DI container, plugin runtime, dashboard-managed configuration, storing application logs in PostgreSQL.
