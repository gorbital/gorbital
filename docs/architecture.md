# apistock Architecture (v2)

**Status:** Accepted · **Date:** 2026-09-14 · **Supersedes:** Architecture v1 (see git history of this file)

This page is the overview. Each decision's detail, alternatives and trade-offs live in the linked [ADRs](adr/README.md). If this page and an ADR disagree, the ADR wins; fix this page.

---

## 1. What apistock is

apistock gives Go developers a production-ready API in minutes, as code they own.

```bash
aps new my-api        # answer a few questions
cd my-api
aps dev               # API, docs and local services running
```

Three products, versioned together ([ADR-0014](adr/0014-product-shape-and-presets.md)):

| Product | What it is | Runs in production? |
|---|---|---|
| **Library** `apistock.dev/...` | Go packages with all reusable and security-sensitive logic (auth, sessions, jobs, audit, lifecycle, HTTP) | Yes, imported by the app |
| **CLI** `aps` | Creates apps, adds features, generates code, upgrades | No |
| **Templates and recipes** | The generated app's owned code, versioned with the library it calls | No (their output does) |

**Core rule: thin glue, thick library.** Apps get working features out of the box, but the logic lives in the library, so security fixes reach every app with `go get`. Generated code is readable glue the developer owns.

**Non-goals:** a hosted platform, a dashboard required to run apps, a mobile app built into apistock itself (dashboard and mobile clients are optional templates, proposed for v1.2 in [ADR-0047](adr/0047-client-templates.md)), microservices tooling, a custom ORM, router or DI container, databases other than PostgreSQL.

---

## 2. Principles

1. **Readable over clever.** A request can be traced from `main.go` to SQL with "go to definition". No reflection wiring, no hidden registration.
2. **Owned code is sacred.** The CLI never silently overwrites developer code; changes are previewed, recorded and merged.
3. **Library for behaviour, generation for wiring.**
4. **Standard first:** stdlib, then de facto standards (pgx, goose, OpenTelemetry, River), then our own code.
5. **Only PostgreSQL required** in production.
6. **Secure and observable by default,** every default visible in code.
7. **Upgrade path before features:** every feature states how existing apps receive it.
8. **The app survives without apistock tooling:** delete the CLI and the app still builds, tests and deploys.

---

## 3. Creating an app

```text
$ aps new my-api
? Preset           › ● Full  ○ Minimal
? Will different companies or teams use your app, each with their own separate data?
                     ● No (single-tenant)  ○ Yes (multi-tenant)
? Email provider   › ● Resend  ○ SMTP
? GitHub repo + CI › ○ No  ● Yes
```

| Preset | Contents |
|---|---|
| **Minimal** | HTTP server, config, logging, tracing, health, security defaults, OpenAPI + `/docs`, Dockerfile. No database; Docker not required. |
| **Full** | Minimal + PostgreSQL, runtime settings, jobs, email, full authentication, users/roles/permissions, tenancy choice, audit logs, operations APIs, seed data, tests, CI option. |
A Custom preset (a feature checklist) isn't planned for v0.5: every offered combination would need its own tested golden app ([ADR-0050](adr/0050-upgrades-and-adding-features.md)).

Every prompt has a flag (`--preset`, `--tenancy`, `--mail`, `--github`, `--yes`) for CI and AI agents. Each preset is a whole template tree generated from a golden app; `aps add` and `aps upgrade` move an app from one tree to another with the same 3-way merge ([ADR-0050](adr/0050-upgrades-and-adding-features.md)).

---

## 4. Ecosystem boundaries

```text
DEVELOPER MACHINE / CI (never in production)        PRODUCTION (any host)
  aps CLI ── recipes via Go module proxy + sumdb        generated app binary
    ├─ generator (plan → validate → preview → apply)       ├─ app code (owned)
    ├─ upgrade (3-way merge on a branch)                   ├─ apistock modules (imported)
    ├─ dev (Docker: postgres, mailpit, grafana opt-in)     └─ apistock core (imported)
    └─ git / gh (optional)                              PostgreSQL (only required service)
                                                        OTLP backend of your choice (optional)
```

| Component | Status | Notes |
|---|---|---|
| Core library, official modules, CLI, recipes | v1 | This repository |
| Community modules | After 1.0 | Same contracts, authors' own repositories |
| Local dev console (custom UI) | v1.1 | v1 uses Mailpit and Grafana containers ([ADR-0028](adr/0028-local-development-environment.md)) |
| Admin web UI | Not in v1 | v1 ships `/ops/*` APIs only ([ADR-0026](adr/0026-operations-apis.md)); a dashboard client template is proposed for v1.2 |
| Hosted control plane | Not planned | Separate product if ever built; standard protocols only |
| Client templates: docs site, dashboard, Expo | v1.2, proposed | Separate template repositories, pinned and verified archives filled in by `aps new` ([ADR-0047](adr/0047-client-templates.md)) |
| Native iOS and Android templates | Later | Only when builders ask; Expo covers both first |
| GitHub integration | v1 | Delegates to `git` and `gh`; plain workflow files ([ADR-0011](adr/0011-github-integration.md)) |

---

## 5. Library architecture

### 5.1 Dependency rules ([ADR-0019](adr/0019-module-dependency-rules.md))

```text
generated app ──► modules/* ──► core ──► stdlib (+ OpenTelemetry API, golang.org/x)
                  modules never import other modules
```

- A contract enters **core** only when at least two official modules consume it.
- **Core dependency budget:** standard library, OpenTelemetry API (and its tiny dependencies), `golang.org/x` packages. `internal/archtest` fails the build otherwise.
- Cross-module needs are solved at the composition root. Example: auth takes a `mail.Sender`; the app passes `jobs.AsyncSender(resend)`.
- CI enforces import rules and the dependency budget.

### 5.2 Repository layout ([ADR-0009](adr/0009-repository-strategy.md))

```text
apistock/
├── go.mod                   module apistock.dev (core)
├── app/                     lifecycle: Runner, cleanup stack, shutdown sequence
├── httpx/                   server, middleware, security headers, CORS, CSRF, problem+json
├── health/                  /livez, /readyz, named checks
├── actor/                   who is acting (context)
├── requestid/               request ID generation, validation, context
├── audit/                   Event, Recorder
├── mail/                    Message, Sender
├── config/                  Secret type, env lookup with _FILE support
├── page/                    cursor pagination, sort
├── ratelimit/               token bucket + middleware
├── buildinfo/               version, commit, build time
├── internal/archtest/       dependency budget test
├── modules/
│   ├── openapi/             Huma integration, problem errors, /docs API reference (reference/)   (v0.1)
│   ├── telemetry/           OpenTelemetry SDK + exporters, correlated logs           (v0.1)
│   ├── postgres/            pool, transactions, migrations runner, pgtest (against Docker PostgreSQL)   (v0.2)
│   ├── settings/            runtime settings: typed declarations, PostgreSQL store, LISTEN/NOTIFY reload   (v0.2)
│   ├── jobs/                River, job definitions and Manager (Lambda-style config), AsyncSender   (v0.2)
│   ├── mail/resend/ · mail/smtp/   Resend HTTP API and standard-library SMTP senders   (v0.2)
│   ├── auditpg/             append-only audit store with redaction, filtered query API   (v0.2)
│   ├── auth/                building blocks: argon2id, tokens, codes, session middleware, permission catalog (v0.2); oidc, totp, passkey (v0.3)
│   ├── orgs/                building blocks: organisation IDs, RequireMember, invitation emails   (v0.4)
│   └── releases/            instance build record at start, heartbeats, release queries   (v0.2)
├── cli/                     module apistock.dev/cli → cmd/aps
│   └── internal/recipes/    templates generated from examples/ (go generate), embedded in aps
├── examples/                hand-written golden apps: minimal (v0.1), full-single (v0.2), full-multi (v0.4, organisations)
├── compose.yaml             PostgreSQL in Docker for module tests (host port 55432)
├── scripts/                 first-run measurement
├── spikes/                  throwaway experiments
├── .github/workflows/       CI and signed aps releases
└── docs/
```

### 5.3 Core contracts

| Contract | Rule | ADR |
|---|---|---|
| Lifecycle | Constructors do setup; `Runner` (`Run(ctx) error`) for long-running work; `io.Closer`; defined shutdown order | [0017](adr/0017-application-lifecycle.md) |
| Errors | Modules expose sentinel and typed errors; driver errors wrapped with `%v`; the app maps errors to problem+json; errors logged once, at the edge | [0018](adr/0018-error-contract.md) |
| Constructors and config | Required dependencies positional; optional settings as functional options; no env tags in libraries | [0020](adr/0020-constructors-and-configuration.md) |
| Runtime settings | Secrets and infrastructure in env; non-secret tunables declared in code, stored in PostgreSQL, edited via `/ops/settings`; live options accept `config.Value[T]` | [0031](adr/0031-runtime-settings.md) |
| Context | Actor, request ID, trace, tenant only; jobs carry them in metadata; `WithoutCancel` for audit writes | [0030](adr/0030-context-and-correlation.md) |
| Interfaces | Small and consumer-owned; `audit.Recorder` and `mail.Sender` have one method each | [0019](adr/0019-module-dependency-rules.md) |
| Logging | Injected `*slog.Logger`; `InfoContext`; log IDs, never emails, tokens or secrets | [0007](adr/0007-observability.md) |

### 5.4 Public API and versioning ([ADR-0015](adr/0015-public-api-and-stability-tiers.md), [ADR-0016](adr/0016-scaffold-compatibility-and-upgrades.md))

- **Tiers:** stable, experimental (`apistock.dev/x`, always v0), internal.
- **Also public API:** CLI commands, flags and `--json` output; manifest and lock formats; anchor syntax; error codes; audit action names; module table ID columns.
- **Everything is v0 until 1.0;** breaking changes in v0 ship with upgrade notes.
- **From 1.0:** scaffold code from template vX.Y works with library vX.Z for every Z ≥ Y. Minor upgrades are a plain `go get`; majors use bridge releases and `aps upgrade --major`.

---

## 6. The generated application ([ADR-0022](adr/0022-generated-application-layout.md))

Layered modules (`domain / usecase / repository / delivery`) inside `internal/modules/`, with `internal/app` as the composition root.

```text
my-api/
├── cmd/{api,worker,migrate,seed}/main.go
├── api/{openapi.json, postman_collection.json, llms.txt}   (exported from code)
├── internal/
│   ├── app/            app.go · config.go · routes.go · infra_*.go · modules.go · module_<name>.go · architecture_test.go
│   ├── modules/
│   │   ├── auth/       module.go · domain/ · usecase/ · repository/ · delivery/
│   │   ├── users/      (same layers)
│   │   ├── orgs/       (multi-tenant only)
│   │   ├── ops/
│   │   └── projects/   example module to copy
│   ├── jobs/           job workers and schedules
│   ├── emails/         templates and dev preview
│   └── wellknown/      apple-app-site-association, assetlinks.json
├── db/migrations/
├── test/{e2e,testutil}/
├── docs/{adr,deployment.md,auth-providers.md,runbook.md}
├── compose.yaml · Dockerfile · .env.example
├── apistock.yaml · apistock.lock · ARCHITECTURE.md · AGENTS.md · README.md
└── go.mod
```

Rules enforced by `architecture_test.go`:

- `domain/` imports only the standard library; domain structs have no `json`/`db` tags.
- `usecase/` imports its own `domain/` and apistock core contracts, and defines its ports in `ports.go`.
- `repository/` implements the ports with hand-written SQL and pgx, one file per operation (`insert_user.go`, `select_user.go`, …), each tested against Docker PostgreSQL ([ADR-0032](adr/0032-repository-sql.md)); `delivery/` defines Huma request/response types and operations, and never imports `repository/`.
- Modules never import other modules.
- Only `internal/app` reads environment variables.

Three kinds of code: **library** (imported), **derived** (`// Code generated … DO NOT EDIT.`), **owned scaffold** (edited freely, upgraded by 3-way merge).

---

## 7. Features

### 7.1 Authentication ([ADR-0024](adr/0024-authentication-methods.md))

Email + password with email verification codes; server-side sessions (no JWT sessions); logout and logout-all; password reset and change; active sessions list and revoke; account deletion; Google and Apple sign-in (web redirect and native token); TOTP with recovery codes; passkeys; platform roles with a permission catalog; 2FA policy per role.

Implemented in v0.2 ([ADR-0038](adr/0038-authentication-v0-2.md), [authentication guide](guides/authentication.md)): the generated app owns `internal/modules/auth` with all four layers (use cases for every flow, a repository with one SQL file per operation, `/v1/auth` endpoints); `modules/auth` provides the building blocks (argon2id, tokens and codes stored as hashes, the session middleware and cookies, the permission catalog, plain emails). Browsers get an HttpOnly `__Host-session` cookie; native clients ask for a bearer token. Durations are runtime settings clamped to hard limits. Roles are read on every request, and the first administrator is granted with `go run ./cmd/api grant-role <email> platform_admin`.

Implemented in v0.3:

- **Two-factor authentication** ([ADR-0043](adr/0043-two-factor-authentication.md)): authenticator apps (TOTP, with the `otpauth://` URI and a scannable QR image), 10 recovery codes, a 202 login challenge completed at `/v1/auth/login/mfa`, secrets encrypted with `AUTH_ENCRYPTION_KEYS`, and required 2FA for `platform_admin` and `ops_viewer`.
- **Passkeys** ([ADR-0044](adr/0044-passkeys.md)): passwordless sign-in and a second factor through `apistock.dev/modules/auth/passkey` (go-webauthn), up to 10 per account, single-use ceremonies stored server-side, relying party from `WEBAUTHN_*`, and the generated `/.well-known/apple-app-site-association` and `assetlinks.json` for native apps.
- **Sign-in provider setup** ([ADR-0045](adr/0045-sign-in-provider-setup.md)): what each method needs from the developer, in `.env.example`, `AUTH_PROVIDERS.md`, a status block at start, `go run ./cmd/api auth-providers` and `GET /ops/auth/providers`.
- **Google and Apple sign-in** ([ADR-0046](adr/0046-google-and-apple-sign-in.md)): `apistock.dev/modules/auth/social` (x/oauth2, go-oidc); the API hosts the web flow (`/v1/auth/{provider}/start` and callbacks, state bound to a `__Host-oauth` cookie) and verifies native apps' ID tokens with single-use nonces; identities link to accounts by provider-verified email; the second factor still applies; Apple tokens are revoked on deletion and Apple's notifications are handled.

### 7.2 Tenancy ([ADR-0023](adr/0023-tenancy.md))

- **Single-tenant** (default) or **multi-tenant**, chosen at creation and stored in `apistock.yaml`.
- Multi-tenant: shared schema with `org_id`; a personal workspace per user; memberships; invitations; org roles separate from platform roles; `/v1/orgs/{orgId}/...` routes.
- Isolation at four layers: membership middleware, repositories that require `OrgID`, composite foreign keys including `org_id`, generated cross-org tests. Row-level security in v1.1.
- `aps add orgs` gives a guided single → multi path. Multi → single is not supported.

### 7.3 Email ([ADR-0025](adr/0025-email-providers.md))

Resend or SMTP behind `mail.Sender`. Development always delivers to Mailpit. Sends run as jobs with idempotency; permanent refusals (`mail.ErrRejected`) are cancelled instead of retried.

Implemented in v0.2 ([ADR-0037](adr/0037-email-setup-and-delivery.md), [email guide](guides/email.md)): `aps add mail` asks Resend or SMTP, saves typed secrets only to `.env`, writes `internal/app/infra_mail.go` and the `.env.example` block, and prints next steps; running it again switches provider. The Resend API key and SMTP credentials are environment variables; the sender name, address and reply-to are runtime settings (`mail.*`) filled into each message by `mail.WithDefaults`. `MAIL_DELIVERY` picks Mailpit (development default) or the provider (always in production).

### 7.4 Operations APIs ([ADR-0026](adr/0026-operations-apis.md))

`/ops/*`, protected by platform roles and required 2FA. v1: audit logs, system health, release monitor, jobs, retention, maintenance mode, runtime settings. v1.1: feature flags, live observability, incidents, API keys.

Implemented in v0.2 (`examples/full-single`, [ops API reference](guides/ops-api.md)): `/ops/settings` ([ADR-0031](adr/0031-runtime-settings.md)), `/ops/jobs/definitions`, `/ops/jobs/scheduled`, `/ops/jobs/runs` and `/ops/queues` ([ADR-0033](adr/0033-background-jobs.md)), `/ops/audit` ([ADR-0036](adr/0036-audit-storage.md)), `/ops/mail` ([ADR-0037](adr/0037-email-setup-and-delivery.md)). They require a signed-in user whose platform roles grant the operation's permission ([ADR-0038](adr/0038-authentication-v0-2.md)); required 2FA for ops roles arrived in v0.3 ([ADR-0043](adr/0043-two-factor-authentication.md)), with `/ops/auth/providers` ([ADR-0045](adr/0045-sign-in-provider-setup.md)).

### 7.5 API contract and docs ([ADR-0027](adr/0027-api-contract-and-docs.md))

Code-first with Huma v2, confined to `delivery/`: developers write Go input/output types and handlers; OpenAPI 3.1, validation and problem+json errors follow automatically. `my-api openapi` exports `api/openapi.json` (committed, checked for breaking changes in CI). `/docs` serves an API reference in the apistock design, rendered from the app's own OpenAPI document by `modules/openapi/reference`, the same renderer as the public API reference ([ADR-0049](adr/0049-public-docs-and-website.md)). The Postman collection and `llms.txt` are generated from the exported spec.

### 7.6 Observability ([ADR-0007](adr/0007-observability.md), [ADR-0028](adr/0028-local-development-environment.md))

`modules/telemetry` keeps OpenTelemetry always on in the app (traces, metrics, slog logs carrying `request_id`, `trace_id` and `span_id`); export is enabled by setting `OTEL_EXPORTER_OTLP_ENDPOINT`. The Minimal preset needs no Docker. From v0.2, `aps dev` starts PostgreSQL and Mailpit, and `aps dev --observability` also starts Grafana (`grafana/otel-lgtm`). PostgreSQL always runs in Docker (the official image, through `compose.yaml` locally and a service container in CI); apistock never downloads PostgreSQL binaries.

### 7.7 Configuration ([ADR-0020](adr/0020-constructors-and-configuration.md), [ADR-0031](adr/0031-runtime-settings.md))

Two layers. **Environment** holds secrets, credentials and infrastructure (database URL, API keys, listen addresses) and changes with a redeploy. **Runtime settings** hold non-secret tunables (expiries, limits, sender names, frontend URLs, maintenance mode): declared in Go as typed handles with defaults and bounds, stored in PostgreSQL only when changed, edited through `PUT /ops/settings/{key}` with a reason and version, recorded in history and the audit log, and applied on every instance through `LISTEN/NOTIFY`. A value is never in both layers, and secrets are never settings. Guide: [runtime settings](guides/runtime-settings.md).

### 7.8 Background jobs ([ADR-0033](adr/0033-background-jobs.md))

Jobs run on PostgreSQL with River, in the API process or a separate worker. Each job is a **definition**, like a serverless function: developers write and deploy the code, and operators change its configuration at runtime (enabled, schedule, timeout, max attempts, queue, priority) through `/ops/jobs`, with versions, reasons, history and audit events. Schedules are 5-field cron in UTC or `@every` intervals, run once across instances by River's elected leader, and never more often than once a minute. Jobs carry the enqueuing request ID, trace and actor but never permissions, and run as the `jobs` system actor. Email is sent through `jobs.AsyncSender` with job-ID idempotency keys. River's tables are migrated by River's migrator from `cmd/migrate`. Guide: [background jobs](guides/background-jobs.md).

### 7.9 Database ([ADR-0005](adr/0005-database-strategy.md), [ADR-0028](adr/0028-local-development-environment.md), [ADR-0032](adr/0032-repository-sql.md))

PostgreSQL only, always from Docker in development, tests and CI. `modules/postgres` opens a traced pgx pool and provides `DBTX`, `InTx`, error classification, goose migrations and `pgtest` (a fresh database per test). Repositories use hand-written SQL with one file per operation. Guide: [database](guides/database.md).

---

## 8. CLI and generator ([ADR-0021](adr/0021-generator-operation-model.md))

| Command | Purpose |
|---|---|
| `aps new <name>` | Create an app (presets and prompts) |
| `aps add <feature>` | Add a feature: `mail` switches the email provider; `orgs` (v0.5) turns a single-tenant app multi-tenant |
| `aps gen resource <Name> <field:type>... [--scope=user]` | One-shot layered module owned by the signed-in user, with table, API and tests ([ADR-0039](adr/0039-resource-module-template.md)); `org` and `global` scopes later |
| `aps gen job <Name> [--cron SPEC\|--every D]` | Job args, worker, test and definition; config editable in `/ops/jobs` ([ADR-0033](adr/0033-background-jobs.md)) |
| `aps gen migration <name>` | Empty forward-only goose migration that runs after the existing ones |
| `aps dev [--observability]` | Run locally with reload and Docker services |
| `aps upgrade [--from <version>] [--major]` | Merge template changes and upgrade the library on branch `aps-upgrade/<version>` (v0.5) |
| `aps doctor` | Check configuration, versions and migrations |

**Implemented in v0.1:** `aps new` (Minimal preset; `--module`, `--local`, `--json`, `--no-git`), `aps dev` (build, run, reload, `.env`, port check), `aps version`.

**Implemented in v0.2 so far:** `aps gen job` (interactive or flags), `aps gen resource` (string, text and enum fields; golden-tested against `examples/full-single`'s projects module), `aps gen migration`, `aps dev` with Docker services, migrations, seed data and `--observability` ([ADR-0042](adr/0042-development-seed-data.md)), interactive `aps new`, `aps new --preset=full` (generated from `examples/full-single`, [ADR-0041](adr/0041-full-preset-generation.md)), `aps add mail`. Guide: [CLI](guides/cli.md).

**Implemented in v0.5 so far:** `apistock.lock` v2 and `aps upgrade` ([ADR-0050](adr/0050-upgrades-and-adding-features.md)).

**Interaction ([ADR-0035](adr/0035-interactive-cli.md)):** in a terminal, commands ask for missing values with arrow-key selects, checkboxes, validated inputs and a final summary; every prompt has a flag, flags skip their prompts, and `--yes`, `--json`, `--no-input` or `CI` never prompt. Prompts and flags share validators.

- **Recipes are whole preset trees** generated from the golden apps ([ADR-0041](adr/0041-full-preset-generation.md)). API endpoints come from Go code (ADR-0027), so recipes never edit a spec file. No code runs at install time.
- **One wiring file per feature** stays a style goal: it keeps merges small.
- **Tracked vs one-shot:** `apistock.lock` v2 records the `aps` release, the template inputs and a hash of every tracked file; `aps upgrade` rebuilds the old tree from that release, proves it against the hashes and merges 3-way ([ADR-0050](adr/0050-upgrades-and-adding-features.md)). `aps gen` output is one-shot.
- **Safety:** names validated as Go identifiers, field types from an allowlist, writes confined with `os.Root`, diff preview with risky new imports highlighted, clean git tree required.

---

## 9. Security ([ADR-0029](adr/0029-threat-model.md))

The threat model covers the framework, CLI and ecosystem, not only generated apps. Highest priorities: `apistock.dev` domain and DNS hardening, template injection, malicious recipes, OAuth and ID-token validation, passkey origin checks, cross-tenant access, ops endpoint protection, release signing and maintainer account security.

---

## 10. Milestones ([roadmap](roadmap.md))

| Release | Delivers |
|---|---|
| v0.1 ✅ implemented, unreleased | Core library, Minimal preset, `aps new` and `aps dev`, OpenAPI + Scalar docs, CI, signed release workflow |
| v0.2 | PostgreSQL, runtime settings, jobs, email (Resend/SMTP), email/password auth, users and roles, audit, Full preset, single-tenant, `aps dev` with Docker, seed data |
| v0.3 | Google, Apple, TOTP, passkeys |
| v0.4 | Multi-tenant organisations, tenancy prompt |
| v0.5 | Operations APIs, Postman, `llms.txt`, `aps upgrade`, `aps add orgs`, `aps doctor` |
| v1.0 | External security review, API freeze, documentation site |
| v1.1 | Feature flags, live observability, API keys, GitHub login, row-level security option, local dev console |
| v1.2 (proposed) | Client templates: docs site, dashboard and Expo app created by `aps new` from separate template repositories ([ADR-0047](adr/0047-client-templates.md)) |

---

## 11. Open items

| Item | Resolved by |
|---|---|
| ~~API contract: spec-first vs code-first~~ | Resolved: code-first with Huma ([spike](../spikes/openapi/README.md), ADR-0027) |
| ~~Unknown request fields: strict vs tolerant~~ | Resolved: tolerant (ADR-0027) |
| ~~Anchor edits: text insertion vs AST~~ | Resolved: parser-located text insertion ([spike](../spikes/anchor/README.md), ADR-0021) |
| ~~Minimal first run under 60 seconds~~ | Resolved: 12.0 s cold, 1.6 s warm in the spike; 25.0 s cold, 4.8 s warm with the real v0.1 CLI (`scripts/first-run.sh`) |
| ~~Scalar docs visual check in a real browser~~ | Resolved: Scalar replaced by the apistock reference, checked in a browser in a generated app and on the site ([ADR-0049](adr/0049-public-docs-and-website.md)) |
| Publish the library at `apistock.dev` | Open: domain hardening, public repository, first tags (until then apps use `--local`) |
| ~~`/ops/*` protection before authentication~~ | Resolved: sessions and platform roles replaced the interim `OPS_TOKEN` ([ADR-0038](adr/0038-authentication-v0-2.md)); ops roles require two-factor authentication ([ADR-0043](adr/0043-two-factor-authentication.md)) |
| ~~Example business module with its own repository~~ | Resolved: `examples/full-single/internal/modules/projects` owns the `projects` table with all four layers, user ownership and cross-owner tests ([ADR-0039](adr/0039-resource-module-template.md)); `aps gen resource` reproduces it exactly |
| ~~`aps new --preset=full`~~ | Resolved: templates generated from `examples/full-single`, reproduced byte for byte, with `go.mod` derived from the golden `go.mod` ([ADR-0041](adr/0041-full-preset-generation.md)) |
| ~~Audit storage~~ | Resolved: `modules/auditpg` stores events in an append-only `audit_events` table, listed by `/ops/audit` ([ADR-0036](adr/0036-audit-storage.md)) |
| ~~Email providers and setup~~ | Resolved: `modules/mail/smtp`, `modules/mail/resend` and `aps add mail` ([ADR-0037](adr/0037-email-setup-and-delivery.md)) |
| Email templates and preview route | Open: owned templates in `internal/emails` with a development preview (ADR-0025) arrive with authentication's emails |
| Client IP and user agent in audit events | Open: no core middleware carries them in the context yet; `modules/auth` sets them on its events |
| Organisations design | Open: [ADR-0048](adr/0048-organisations-v0-4.md) proposed; five questions for the maintainer (role per member, staff access, personal workspace invitations, invited-email match, `aps add orgs` in v0.5) |
| Client templates | Open: [ADR-0047](adr/0047-client-templates.md) proposed; to decide the dashboard and docs stacks, where archives are hosted, and the bundle ID prompt before accepting |
