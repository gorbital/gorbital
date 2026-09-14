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

**Non-goals:** a hosted platform, a dashboard required to run apps, a mobile app, microservices tooling, a custom ORM, router or DI container, databases other than PostgreSQL.

---

## 2. Principles

1. **Readable over clever.** A request can be traced from `main.go` to SQL with "go to definition". No reflection wiring, no hidden registration.
2. **Owned code is sacred.** The CLI never silently overwrites developer code; changes are previewed, recorded and merged.
3. **Library for behaviour, generation for wiring.**
4. **Standard first:** stdlib, then de facto standards (pgx, sqlc, OpenTelemetry, River), then our own code.
5. **Only PostgreSQL required** in production.
6. **Secure and observable by default,** every default visible in code.
7. **Upgrade path before features:** every feature states how existing apps receive it.
8. **The app survives without apistock tooling:** delete the CLI and the app still builds, tests and deploys.

---

## 3. Creating an app

```text
$ aps new my-api
? Preset           › ● Full  ○ Minimal  ○ Custom
? Will different companies or teams use your app, each with their own separate data?
                     ● No (single-tenant)  ○ Yes (multi-tenant)
? Email provider   › ● Resend  ○ SMTP
? GitHub repo + CI › ○ No  ● Yes
```

| Preset | Contents |
|---|---|
| **Minimal** | HTTP server, config, logging, tracing, health, security defaults, OpenAPI + `/docs`, Dockerfile. No database; Docker not required. |
| **Full** | Minimal + PostgreSQL, jobs, email, full authentication, users/roles/permissions, tenancy choice, audit logs, operations APIs, seed data, tests, CI option. |
| **Custom** | A checklist of features; required dependencies are added automatically (for example passkeys ⇒ auth ⇒ PostgreSQL, email, jobs). |

Every prompt has a flag (`--preset`, `--tenancy`, `--mail`, `--github`, `--yes`) for CI and AI agents. `aps new` applies the base recipe plus the selected feature recipes with **the same engine as `aps add`** ([ADR-0021](adr/0021-generator-operation-model.md)).

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
| Admin web UI | Not in v1 | v1 ships `/ops/*` APIs only ([ADR-0026](adr/0026-operations-apis.md)) |
| Hosted control plane | Not planned | Separate product if ever built; standard protocols only |
| Mobile app | No | |
| GitHub integration | v1 | Delegates to `git` and `gh`; plain workflow files ([ADR-0011](adr/0011-github-integration.md)) |

---

## 5. Library architecture

### 5.1 Dependency rules ([ADR-0019](adr/0019-module-dependency-rules.md))

```text
generated app ──► modules/* ──► core ──► stdlib (+ OpenTelemetry API, golang.org/x)
                  modules never import other modules
```

- A contract enters **core** only when at least two official modules consume it.
- **Core dependency budget:** standard library, OpenTelemetry API, `golang.org/x` packages.
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
├── audit/                   Event, Recorder
├── mail/                    Message, Sender
├── config/                  Secret type
├── page/                    cursor pagination, filter, sort
├── ratelimit/               token bucket + middleware
├── buildinfo/               version, commit, build time
├── modules/
│   ├── otel/                OpenTelemetry SDK + exporters
│   ├── postgres/            pool, transactions, migrations runner, pgtest
│   ├── jobs/                River, cron, AsyncSender
│   ├── mail/resend/ · mail/smtp/
│   ├── auditpg/             audit store + query API
│   ├── auth/                identity, passwords, sessions, oidc (google, apple), totp, passkey
│   ├── orgs/                organisations, memberships, invitations, org roles
│   └── releases/            release record at boot + query API
├── cli/                     module apistock.dev/cli → cmd/aps
├── recipes/                 base-minimal, feature recipes, resource templates, presets
├── examples/                hand-written golden apps: minimal, full-single, full-multi
├── spikes/                  throwaway experiments
└── docs/
```

### 5.3 Core contracts

| Contract | Rule | ADR |
|---|---|---|
| Lifecycle | Constructors do setup; `Runner` (`Run(ctx) error`) for long-running work; `io.Closer`; defined shutdown order | [0017](adr/0017-application-lifecycle.md) |
| Errors | Modules expose sentinel and typed errors; driver errors wrapped with `%v`; the app maps errors to problem+json; errors logged once, at the edge | [0018](adr/0018-error-contract.md) |
| Constructors and config | Required dependencies positional; optional settings as functional options; no env tags in libraries | [0020](adr/0020-constructors-and-configuration.md) |
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
│   ├── app/            app.go · config.go · routes.go · errors.go · infra_*.go · modules.go · architecture_test.go
│   ├── modules/
│   │   ├── auth/       module.go · domain/ · usecase/ · repository/ · delivery/
│   │   ├── users/      (same layers)
│   │   ├── orgs/       (multi-tenant only)
│   │   ├── ops/
│   │   └── projects/   example module to copy
│   ├── jobs/           job workers and schedules
│   ├── emails/         templates and dev preview
│   ├── wellknown/      apple-app-site-association, assetlinks.json
│   └── db/             GENERATED by sqlc
├── db/{migrations,queries}/
├── test/{e2e,testutil}/
├── docs/{adr,deployment.md,auth-providers.md,runbook.md}
├── compose.yaml · Dockerfile · .env.example · sqlc.yaml
├── apistock.yaml · apistock.lock · ARCHITECTURE.md · AGENTS.md · README.md
└── go.mod
```

Rules enforced by `architecture_test.go`:

- `domain/` imports only the standard library; domain structs have no `json`/`db` tags.
- `usecase/` imports its own `domain/` and apistock core contracts, and defines its ports in `ports.go`.
- `repository/` implements the ports (sqlc, pgx); `delivery/` defines Huma request/response types and operations, and never imports `repository/`.
- Modules never import other modules.
- Only `internal/app` reads environment variables.

Three kinds of code: **library** (imported), **derived** (`// Code generated … DO NOT EDIT.`), **owned scaffold** (edited freely, upgraded by 3-way merge).

---

## 7. Features

### 7.1 Authentication ([ADR-0024](adr/0024-authentication-methods.md))

Email + password with email verification codes; server-side sessions (no JWT sessions); logout and logout-all; password reset and change; active sessions list and revoke; account deletion; Google and Apple sign-in (web redirect and native token); TOTP with recovery codes; passkeys; platform roles with a permission catalog; 2FA policy per role.

### 7.2 Tenancy ([ADR-0023](adr/0023-tenancy.md))

- **Single-tenant** (default) or **multi-tenant**, chosen at creation and stored in `apistock.yaml`.
- Multi-tenant: shared schema with `org_id`; a personal workspace per user; memberships; invitations; org roles separate from platform roles; `/v1/orgs/{orgId}/...` routes.
- Isolation at four layers: membership middleware, repositories that require `OrgID`, composite foreign keys including `org_id`, generated cross-org tests. Row-level security in v1.1.
- `aps add orgs` gives a guided single → multi path. Multi → single is not supported.

### 7.3 Email ([ADR-0025](adr/0025-email-providers.md))

Resend or SMTP behind `mail.Sender`. Development always delivers to Mailpit. Sends run as jobs with idempotency.

### 7.4 Operations APIs ([ADR-0026](adr/0026-operations-apis.md))

`/ops/*`, protected by platform roles and required 2FA. v1: audit logs, system health, release monitor, jobs overview, retention, maintenance mode. v1.1: configuration center, feature flags, live observability, incidents, API keys.

### 7.5 API contract and docs ([ADR-0027](adr/0027-api-contract-and-docs.md))

Code-first with Huma v2, confined to `delivery/`: developers write Go input/output types and handlers; OpenAPI 3.1, validation and problem+json errors follow automatically. `my-api openapi` exports `api/openapi.json` (committed, checked for breaking changes in CI). Embedded Scalar serves `/docs`. The Postman collection and `llms.txt` are generated from the exported spec.

### 7.6 Observability ([ADR-0007](adr/0007-observability.md), [ADR-0028](adr/0028-local-development-environment.md))

OpenTelemetry is always on in the app (traces, metrics, correlated slog logs; exporters configured with `OTEL_*` env vars). Locally, `aps dev --observability` also starts Grafana (`grafana/otel-lgtm`); plain `aps dev` starts PostgreSQL and Mailpit only.

---

## 8. CLI and generator ([ADR-0021](adr/0021-generator-operation-model.md))

| Command | Purpose |
|---|---|
| `aps new <name>` | Create an app (presets and prompts) |
| `aps add <feature>` | Add a feature recipe |
| `aps gen resource <Name> [fields] [--scope=org\|user\|global]` | One-shot layered module |
| `aps gen migration <name>` | Empty timestamped migration |
| `aps dev [--observability]` | Run locally with reload and Docker services |
| `aps upgrade [--major]` | Upgrade recipes and library on a branch |
| `aps doctor` | Check configuration, versions and migrations |

- **Recipes** are declarative: `createFile`, `insertLine@anchor`, `addRequire`, `copyMigration`, `appendEnv`. API endpoints come from Go code (ADR-0027), so recipes never edit a spec file. No code runs at install time.
- **One wiring file per feature** plus one call line at one anchor.
- **Tracked vs one-shot:** recipe output is tracked in `apistock.lock` (operations and versions) for upgrades; `aps gen resource` output is one-shot.
- **Safety:** names validated as Go identifiers, field types from an allowlist, writes confined with `os.Root`, diff preview with risky new imports highlighted, clean git tree required.

---

## 9. Security ([ADR-0029](adr/0029-threat-model.md))

The threat model covers the framework, CLI and ecosystem, not only generated apps. Highest priorities: `apistock.dev` domain and DNS hardening, template injection, malicious recipes, OAuth and ID-token validation, passkey origin checks, cross-tenant access, ops endpoint protection, release signing and maintainer account security.

---

## 10. Milestones ([roadmap](roadmap.md))

| Release | Delivers |
|---|---|
| v0.1 | Core library, Minimal preset, `aps new` and `aps dev`, OpenAPI + Scalar docs |
| v0.2 | PostgreSQL, jobs, email (Resend/SMTP), email/password auth, users and roles, audit, Full and Custom presets, single-tenant |
| v0.3 | Google, Apple, TOTP, passkeys |
| v0.4 | Multi-tenant organisations |
| v0.5 | Operations APIs, Postman, `llms.txt`, `aps upgrade` |
| v1.0 | External security review, API freeze, documentation site |

---

## 11. Open items

| Item | Resolved by |
|---|---|
| ~~API contract: spec-first vs code-first~~ | Resolved: code-first with Huma ([spike](../spikes/openapi/README.md), ADR-0027) |
| ~~Unknown request fields: strict vs tolerant~~ | Resolved: tolerant (ADR-0027) |
| ~~Anchor edits: text insertion vs AST~~ | Resolved: parser-located text insertion ([spike](../spikes/anchor/README.md), ADR-0021) |
| Minimal first run under 60 seconds | First-run spike |
