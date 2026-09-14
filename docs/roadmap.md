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

**Status: in progress.** Done (2026-09-14): `modules/postgres` (pool with tracing, `DBTX`, `InTx`, error classification, goose migrations with advisory lock, readiness check, `pgtest` against Docker PostgreSQL), `modules/settings` and core `config.Value[T]` (typed declarations, PostgreSQL store with version checks, history and audit events, LISTEN/NOTIFY with periodic resync; [ADR-0031](adr/0031-runtime-settings.md)), [ADR-0032](adr/0032-repository-sql.md) repository style; `modules/jobs` library (River client with context propagation and graceful stop, job definitions with runtime-editable config and schedules, `Manager` for the admin panel, `AsyncSender`, River migrations; [ADR-0033](adr/0033-background-jobs.md)). `examples/full-single` golden app wiring postgres, runtime settings and jobs, with `cmd/migrate`, `compose.yaml` and the `/ops/settings`, `/ops/jobs/*` and `/ops/queues` admin APIs behind an interim `OPS_TOKEN` (replaced by platform roles when `modules/auth` lands). `aps gen job` and interactive `aps new`, with every prompt available as a flag ([ADR-0035](adr/0035-interactive-cli.md)). `modules/auditpg` (append-only `audit_events` table, `Record` and transactional `RecordTx`, metadata redaction and size bounds, filtered and paginated `List`, `Get`; [ADR-0036](adr/0036-audit-storage.md)), wired into `examples/full-single` with `GET /ops/audit` and `GET /ops/audit/{id}` (moved from v0.5). Email: `modules/mail/smtp` and `modules/mail/resend`, core `mail.ErrRejected` and `mail.WithDefaults`, rejected sends cancelled by the mail worker, Mailpit in development, sender as runtime settings, `GET /ops/mail` and `POST /ops/mail/test`, and interactive `aps add mail` to choose or switch Resend or SMTP ([ADR-0037](adr/0037-email-setup-and-delivery.md)). Authentication (2026-09-15, [ADR-0038](adr/0038-authentication-v0-2.md)): `modules/auth` building blocks (argon2id hashing, tokens and codes stored as hashes, session middleware and cookies, permission catalog, emails) and, in `examples/full-single`, an app-owned `internal/modules/auth` with domain, use cases, repository (one SQL file per operation) and `/v1/auth` endpoints: register, email codes, login with cookie or bearer token, logout and logout-all, sessions, password reset and change, account deletion, platform roles (`platform_admin`, `ops_viewer`) granted with `go run ./cmd/api grant-role`, and the `auth_cleanup` job. `OPS_TOKEN` is gone: `/ops/*` uses sessions and roles. Example resource (2026-09-15, [ADR-0039](adr/0039-resource-module-template.md)): `internal/modules/projects` in `examples/full-single`, owned by the signed-in user, with all four layers, its own `projects` table and repository, keyset pagination through `page`, versioned `PATCH`, audit events, cross-owner isolation tests and an end-to-end test; the template for `aps gen resource`. Next: `aps gen resource` golden-tested against it, `modules/releases`, then `aps new --preset=full`.

| | |
|---|---|
| **Delivers** | `modules/postgres`, `modules/settings` (runtime settings declared in code, stored in PostgreSQL, live on every instance, `/ops/settings` API, `config.Value[T]` in core; [ADR-0031](adr/0031-runtime-settings.md)), `modules/jobs` (River, AsyncSender, job definitions with runtime-editable schedule, enabled, timeout and retries; `/ops/jobs` admin APIs; [ADR-0033](adr/0033-background-jobs.md)), `modules/mail/resend` and `mail/smtp`, `modules/auditpg`, `modules/auth` (email/password, email codes, sessions, logout-all, reset/change password, active sessions, delete account, platform roles, permission catalog), `modules/releases`, Full and Custom presets, tenancy prompt with single-tenant generation, `aps dev` with Docker (PostgreSQL, Mailpit, `--observability` Grafana), `aps gen resource`, `aps gen job` and `aps gen migration`, seed data, `examples/full-single` |
| **Not included** | Social login, 2FA, passkeys, multi-tenant, ops APIs other than `/ops/settings`, `/ops/jobs/*`, `/ops/queues`, `/ops/audit` and `/ops/mail` (audit stats and retention stay in v0.5) |
| **Done when** | From a fresh `aps new --preset=full`: register → verify email code → login → role-protected endpoint → audit event recorded, all in e2e tests; a setting changed through `/ops/settings` on one instance is served by a second instance without restart and appears in history and the audit log; a job generated with `aps gen job` can be rescheduled, disabled and run now through `/ops/jobs` without a restart; nullable `org_id` and role-scope columns present in library tables; threat model rows 12, 13, 19, 23 and 24 addressed |

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
| **Not included** | Feature flags, live observability, incidents |
| **Done when** | An app generated with v0.2 and edited by script upgrades to v0.5 in CI with no lost edits; ops endpoints require platform roles and 2FA |

## v1.0: Stable

| | |
|---|---|
| **Delivers** | External security review with findings fixed, API freeze and stability tiers in force, `apistock.dev` docs site (Mintlify), domain hardening complete, governance and contribution guide |
| **Done when** | Security review signed off; `gorelease` baseline recorded; scaffold compatibility promise ([ADR-0016](adr/0016-scaffold-compatibility-and-upgrades.md)) active |

## v1.1

Feature flags, per-org settings, live observability, incident reports, API keys and service accounts, GitHub login, PostgreSQL row-level security option, idempotency keys, custom local dev console, Resend bounce/complaint webhooks, Prometheus `/metrics` option.

## Later

Community module index and author tooling, subdomain tenant resolution, per-org quotas and billing, storage and outgoing webhooks modules, enterprise SSO integration, WASI-sandboxed generators.

## Not planned

Admin web UI inside generated apps, hosted control plane, mobile app, databases other than PostgreSQL, schema- or database-per-tenant, custom router, ORM or DI container, plugin runtime, secrets or infrastructure configuration stored in the database, storing application logs in PostgreSQL.
