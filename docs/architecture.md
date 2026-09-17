# gorbital Architecture (v2)

**Status:** Accepted · **Date:** 2026-09-14 · **Supersedes:** Architecture v1 (see git history of this file)

This page is the overview. Each decision's detail, alternatives and trade-offs live in the linked [ADRs](adr/README.md). If this page and an ADR disagree, the ADR wins; fix this page.

---

## 1. What gorbital is

gorbital gives Go developers a production-ready API in minutes, as code they own.

```bash
orb new my-api        # answer a few questions
cd my-api
orb dev               # API, docs and local services running
```

Three products, versioned together ([ADR-0014](adr/0014-product-shape-and-presets.md)):

| Product | What it is | Runs in production? |
|---|---|---|
| **Library** `gorbital.dev/...` | Go packages with all reusable and security-sensitive logic (auth, sessions, jobs, audit, lifecycle, HTTP) | Yes, imported by the app |
| **CLI** `orb` | Creates apps, adds features, generates code, upgrades | No |
| **Templates and recipes** | The generated app's owned code, versioned with the library it calls | No (their output does) |

**Core rule: thin glue, thick library.** Apps get working features out of the box, but the logic lives in the library, so security fixes reach every app with `go get`. Generated code is readable glue the developer owns.

**Non-goals:** a hosted platform, a dashboard required to run apps, a mobile app built into gorbital itself (dashboard and mobile clients are optional templates, proposed in [ADR-0047](adr/0047-client-templates.md)), microservices tooling, a custom ORM, router or DI container, databases other than PostgreSQL.

---

## 2. Principles

1. **Readable over clever.** A request can be traced from `main.go` to SQL with "go to definition". No reflection wiring, no hidden registration.
2. **Owned code is sacred.** The CLI never silently overwrites developer code; changes are previewed, recorded and merged.
3. **Library for behaviour, generation for wiring.**
4. **Standard first:** stdlib, then de facto standards (pgx, goose, OpenTelemetry, River), then our own code.
5. **Only PostgreSQL required** in production.
6. **Secure and observable by default,** every default visible in code.
7. **Upgrade path before features:** every feature states how existing apps receive it.
8. **The app survives without gorbital tooling:** delete the CLI and the app still builds, tests and deploys.

---

## 3. Creating an app

```text
$ orb new
✓ app name … my-api
✓ Go module path … github.com/acme/my-api
✓ preset … full
✓ tenancy … single
✓ gorbital checkout … /Users/you/code/gorbital
✓ initialise a git repository? … yes
? create my-api in ./my-api? … yes  no
```

The email provider isn't asked at creation: new Full apps use Resend, and `orb add mail` switches to SMTP. A GitHub repository and CI option ([ADR-0011](adr/0011-github-integration.md)) isn't implemented yet.

| Preset | Contents |
|---|---|
| **Minimal** | HTTP server, config, logging, tracing, health, security defaults, OpenAPI + `/docs`, Dockerfile. No database; Docker not required. |
| **Full** | Minimal + PostgreSQL, runtime settings, jobs, email, full authentication, users/roles/permissions, tenancy choice, audit logs, operations APIs, seed data, tests, CI option. |
A Custom preset (a feature checklist) isn't planned: every offered combination would need its own tested golden app ([ADR-0050](adr/0050-upgrades-and-adding-features.md)).

Every prompt has a flag (`--module`, `--preset`, `--tenancy`, `--local`, `--no-git`, `--yes`) for CI and AI agents. Each preset is a whole template tree generated from a golden app; `orb add` and `orb upgrade` move an app from one tree to another with the same 3-way merge ([ADR-0050](adr/0050-upgrades-and-adding-features.md)).

---

## 4. Ecosystem boundaries

```text
DEVELOPER MACHINE / CI (never in production)        PRODUCTION (any host)
  orb CLI ── recipes via Go module proxy + sumdb        generated app binary
    ├─ generator (plan → validate → preview → apply)       ├─ app code (owned)
    ├─ upgrade (3-way merge on a branch)                   ├─ gorbital modules (imported)
    ├─ dev (Docker: postgres, mailpit, grafana opt-in)     └─ gorbital core (imported)
    └─ git / gh (optional)                              PostgreSQL (only required service)
                                                        OTLP backend of your choice (optional)
```

| Component | Status | Notes |
|---|---|---|
| Core library, official modules, CLI, recipes | In scope | This repository |
| Community modules | After `v1.0.0` | Same contracts, authors' own repositories |
| Local dev console APIs | Built, in v0.1.0 | Development-only `/_dev/` JSON APIs in `modules/devconsole` for local tools ([ADR-0065](adr/0065-local-dev-console-apis.md)); no console UI is built, so Mailpit and Grafana containers stay the local viewers ([ADR-0028](adr/0028-local-development-environment.md)) |
| Admin web UI | Not planned | gorbital ships `/ops/*` APIs only ([ADR-0026](adr/0026-operations-apis.md)); a dashboard client template is proposed |
| Hosted control plane | Not planned | Separate product if ever built; standard protocols only |
| Client templates: docs site, dashboard, Expo | Proposed | Separate template repositories, pinned and verified archives filled in by `orb new` ([ADR-0047](adr/0047-client-templates.md)) |
| Native iOS and Android templates | Later | Only when builders ask; Expo covers both first |
| GitHub integration | In scope | Delegates to `git` and `gh`; plain workflow files ([ADR-0011](adr/0011-github-integration.md)) |

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
gorbital/
├── go.mod                   module gorbital.dev (core)
├── app/                     lifecycle: Runner, cleanup stack, shutdown sequence
├── httpx/                   server, middleware, security headers, CORS, CSRF, problem+json
├── health/                  /livez, /readyz, named checks
├── actor/                   who is acting, and from which client (context)
├── requestid/               request ID generation, validation, context
├── audit/                   Event, Recorder
├── mail/                    Message, Sender, WithDefaults, WithSuppressionList
├── config/                  Secret type, env lookup with _FILE support
├── page/                    cursor pagination, sort
├── ratelimit/               token bucket + middleware
├── buildinfo/               version, commit, build time
├── internal/archtest/       dependency budget, stability markers, golden apps don't drift
├── internal/tools/apicheck/  module: records and checks the exported API in api/*.txt (ADR-0054)
├── internal/tools/refdocs/   module: generates and checks docs/reference from the golden apps
├── internal/tools/contracts/ module: checks the golden apps against the frozen v0.1.0 contracts in internal/contracts/v0.1.0
├── api/                     exported Go API listings per library module (gorbital.dev.txt, modules-auth.txt, …)
├── modules/
│   ├── openapi/             Huma integration, problem errors, /docs API reference (reference/)
│   ├── telemetry/           OpenTelemetry SDK + OTLP and Prometheus exporters, runtime metrics, correlated logs
│   ├── postgres/            pool, transactions, migrations runner, pgtest (against Docker PostgreSQL), pool metrics, organisation on every connection for row-level security
│   ├── settings/            runtime settings: typed declarations, PostgreSQL store, LISTEN/NOTIFY reload, per-organisation values
│   ├── flags/               feature flags: declared in code, organisation and user targeting, stable percentage rollouts, LISTEN/NOTIFY reload
│   ├── jobs/                River, job definitions and Manager (Lambda-style config), AsyncSender
│   ├── mail/resend/ · mail/smtp/   Resend HTTP API and standard-library SMTP senders, Resend webhook verification
│   ├── mail/suppressionpg/  email suppression list: bounced and complained addresses, once-per-delivery webhook adds
│   ├── observability/       request counts per minute and route shared by every instance, incidents, detection, stream limits
│   ├── devconsole/          development-only /_dev/ APIs: Host, loopback and token checks, request and log ring buffers with SSE streams, configuration without secrets, Mailpit reader
│   ├── auditpg/             append-only audit store with redaction, filtered query API
│   ├── auth/                building blocks: argon2id, tokens, codes, session middleware, permission catalog, oidc, totp, passkey, API keys, GitHub in social
│   ├── orgs/                building blocks: organisation IDs, RequireMember/Authorize, invitation emails
│   ├── ratelimitpg/         rate limits shared across instances: GCRA in an unlogged table, in-memory fallback
│   ├── idempotency/         Idempotency-Key middleware: per-caller keys, atomic claim, stored responses replayed
│   └── releases/            instance build record at start, heartbeats, release queries
├── cli/                     module gorbital.dev/cli → cmd/orb
│   └── internal/recipes/    templates generated from examples/ (go generate), embedded in orb
├── examples/                hand-written golden apps: minimal, full-single, full-multi (organisations)
├── compose.yaml             PostgreSQL in Docker for module tests (host port 55432)
├── scripts/                 first-run measurement
├── spikes/                  throwaway experiments
├── .github/workflows/       CI, signed orb releases, gorelease on library tags
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

- **Tiers:** stable, experimental (`gorbital.dev/x`, always v0; also `modules/devconsole`, [ADR-0065](adr/0065-local-dev-console-apis.md)), internal.
- **Also public API:** CLI commands, flags and `--json` output; manifest and lock formats; anchor syntax; error codes; audit action names; module table ID columns.
- **Everything is v0 until `v1.0.0`;** `v0.1.0` is the first public release. v0 makes no compatibility promise, but the compatibility checks already run, and breaking changes in v0 are listed in the changelog and ship with upgrade notes.
- **From `v1.0.0`:** scaffold code from template vX.Y works with library vX.Z for every Z ≥ Y. Minor upgrades are a plain `go get`; majors use bridge releases and `orb upgrade --major`.

---

## 6. The generated application ([ADR-0022](adr/0022-generated-application-layout.md))

Layered modules (`domain / usecase / repository / delivery`) inside `internal/modules/`, with `internal/app` as the composition root.

The tree of a new single-tenant Full app, as generated from `examples/full-single`:

```text
my-api/
├── cmd/
│   ├── api/main.go        server, and commands: openapi, roles, grant-role, revoke-role, reset-mfa, rotate-auth-keys, auth-providers
│   ├── migrate/main.go    goose migrations, then River's
│   └── seed/main.go       development administrator and example data
├── api/                 openapi.json, Postman collection and llms.txt (go run ./cmd/api openapi --dir api);
│                          surface.json and openapi.baseline.json, the public names and /ops contract checked by tests
├── internal/
│   ├── app/               composition root, the only package that reads the environment
│   │   ├── app.go · config.go · routes.go · modules.go · jobs.go · settings.go · permissions.go
│   │   ├── infra_mail.go · mail.go · keys.go · passkeys.go · social.go · providers.go
│   │   ├── module_<name>.go · job_<name>.go · commands.go · admin.go · admin_mfa.go · migrate.go · seed.go
│   │   └── *_test.go      end-to-end HTTP tests, architecture_test.go (import rules)
│   ├── modules/           each: module.go · domain/ · usecase/ · repository/ · delivery/
│   │   ├── auth/          accounts, sessions, codes, 2FA, passkeys, Google, Apple and GitHub, API keys and service accounts, platform roles
│   │   ├── flags/         GET /v1/flags: client flags evaluated for the caller
│   │   ├── ops/           /ops/* endpoints over the library's managers
│   │   ├── ping/          example endpoint reading a runtime setting
│   │   └── projects/      example resource to copy (orb gen resource output)
│   └── jobs/              job workers: authcleanup/, heartbeat/
├── db/migrations/         goose SQL files, embedded by migrations.go
├── compose.yaml · Dockerfile · .env.example · .gitignore · .dockerignore
├── gorbital.yaml · gorbital.lock
├── README.md · ARCHITECTURE.md · AUTH_PROVIDERS.md · AGENTS.md
└── go.mod · go.sum
```

A multi-tenant app adds `internal/modules/orgs/` and the `orgs_purge` job. The `.well-known` files are served by `mountWellKnown` in `internal/app/passkeys.go`. There's no separate worker command: jobs run in the API process. Emails are rendered by the core `mail` package's layout (`mail.Brand`, [ADR-0078](adr/0078-branded-email-layout.md)) from `internal/app/mail.go`'s brand; the auth and organisation modules take it (`NewBrandedEmails`) and the Dev Portal previews every message. `go run ./cmd/api openapi --dir api` writes `api/openapi.json`, a Postman collection and `llms.txt`; `api/surface.json` and `api/openapi.baseline.json` record the public surface ([ADR-0054](adr/0054-api-freeze-and-scaffold-compatibility.md)).

Rules enforced by `architecture_test.go`:

- `domain/` imports only the standard library; domain structs have no `json`/`db` tags.
- `usecase/` imports its own `domain/` and gorbital core contracts, and defines its ports in `ports.go`.
- `repository/` implements the ports with hand-written SQL and pgx, one file per operation (`insert_user.go`, `select_user.go`, …), each tested against Docker PostgreSQL ([ADR-0032](adr/0032-repository-sql.md)); `delivery/` defines Huma request/response types and operations, and never imports `repository/`.
- Modules never import other modules.
- Only `internal/app` reads environment variables.

Three kinds of code: **library** (imported), **derived** (`// Code generated … DO NOT EDIT.`), **owned scaffold** (edited freely, upgraded by 3-way merge).

---

## 7. Features

### 7.1 Authentication ([ADR-0024](adr/0024-authentication-methods.md))

Email + password with email verification codes; server-side sessions (no JWT sessions); logout and logout-all; password reset and change; active sessions list and revoke; account deletion; Google and Apple sign-in (web redirect and native token); GitHub sign-in (web redirect); API keys and service accounts; TOTP with recovery codes; passkeys; platform roles with a permission catalog; 2FA policy per role.

Built in the data and identity milestone ([ADR-0038](adr/0038-authentication-v0-2.md), [authentication guide](guides/authentication.md)): the generated app owns `internal/modules/auth` with all four layers (use cases for every flow, a repository with one SQL file per operation, `/v1/auth` endpoints); `modules/auth` provides the building blocks (argon2id, tokens and codes stored as hashes, the session middleware and cookies, the permission catalog, plain emails). Browsers get an HttpOnly `__Host-session` cookie; native clients ask for a bearer token. Durations are runtime settings clamped to hard limits. Roles are read on every request, and the first administrator is granted with `go run ./cmd/api grant-role <email> platform_admin`.

Added in the strong authentication milestone:

- **Two-factor authentication** ([ADR-0043](adr/0043-two-factor-authentication.md)): authenticator apps (TOTP, with the `otpauth://` URI and a scannable QR image), 10 recovery codes, a 202 login challenge completed at `/v1/auth/login/mfa`, secrets encrypted with `AUTH_ENCRYPTION_KEYS`, and required 2FA for `platform_admin` and `ops_viewer`.
- **Passkeys** ([ADR-0044](adr/0044-passkeys.md)): passwordless sign-in and a second factor through `gorbital.dev/modules/auth/passkey` (go-webauthn), up to 10 per account, single-use ceremonies stored server-side, relying party from `WEBAUTHN_*`, and the generated `/.well-known/apple-app-site-association` and `assetlinks.json` for native apps.
- **Sign-in provider setup** ([ADR-0045](adr/0045-sign-in-provider-setup.md)): what each method needs from the developer, in `.env.example`, `AUTH_PROVIDERS.md`, a status block at start, `go run ./cmd/api auth-providers` and `GET /ops/auth/providers`.
- **Google and Apple sign-in** ([ADR-0046](adr/0046-google-and-apple-sign-in.md)): `gorbital.dev/modules/auth/social` (x/oauth2, go-oidc); the API hosts the web flow (`/v1/auth/{provider}/start` and callbacks, state bound to a `__Host-oauth` cookie) and verifies native apps' ID tokens with single-use nonces; identities link to accounts by provider-verified email; the second factor still applies; Apple tokens are queued for revocation in the same transaction as unlinking or account deletion and revoked by the `auth_revoke_tokens` job with retries, and Apple's notifications are handled.

Added in the operations and integrations milestone:

- **API keys and service accounts** ([ADR-0058](adr/0058-api-keys-and-service-accounts.md), [guide](guides/api-keys.md)): `modules/auth` generates, parses and compares `gbk_` keys and its middleware routes them to the app's `AuthenticateAPIKey`, never to sessions; the app's auth module stores keys (hashed) and service accounts, builds principals from the owner's current roles without 2FA-required roles, limited to the key's scopes, and keeps account management session-only. Organisation service accounts reach org-scoped modules through `orgs.Service().Memberships()` and are refused outside their organisation by `orgs.Authorize`. Every signed-in operation checks a permission: the `user` role, held by every user, grants what any user may do (user-scoped resources, creating and listing organisations, reading client flags), so a key's scopes bound everything it does; joining and leaving organisations are session-only.
- **GitHub sign-in** ([ADR-0059](adr/0059-github-sign-in.md), [guide](sign-in/github.md)): `social.NewGitHub` in the same package, OAuth 2.0 with state and PKCE and GitHub's user and email API instead of ID tokens; GitHub is never authoritative for an address, so it never links an existing account and creates unverified accounts; signed-in users link it through `POST /v1/auth/github/link`, bound to the browser and the session. Browser sign-ins without `return_to` end at `AUTH_DEFAULT_RETURN_TO`, required in production.

### 7.2 Tenancy ([ADR-0023](adr/0023-tenancy.md))

- **Single-tenant** (default) or **multi-tenant**, chosen at creation and stored in `gorbital.yaml`.
- Multi-tenant: shared schema with `org_id`; a personal workspace per user; memberships; invitations; org roles separate from platform roles; `/v1/orgs/{orgId}/...` routes.
- Isolation at four layers: membership middleware, repositories that require `OrgID`, composite foreign keys including `org_id`, generated cross-org tests.
- A fifth, optional layer: row-level security ([ADR-0061](adr/0061-row-level-security.md), [guide](guides/row-level-security.md)). Every connection carries the organisation of its context in `gorbital.org_id`, and `orb add rls` adds a migration forcing an `org_isolation` policy on every table with `org_id NOT NULL` (memberships and invitations aside), so a query that forgets its organisation filter sees only its own organisation's rows. System paths bypass with `postgres.WithoutRowLevelSecurity`, which is logged; migrations do. The app's database role must not be a superuser or have `BYPASSRLS`; the app warns at startup otherwise.
- `orb add orgs` gives a guided single → multi path. Multi → single is not supported.

### 7.3 Email ([ADR-0025](adr/0025-email-providers.md))

Resend or SMTP behind `mail.Sender`. Development always delivers to Mailpit. Sends run as jobs with idempotency; permanent refusals (`mail.ErrRejected`) are cancelled instead of retried.

Built in the data and identity milestone ([ADR-0037](adr/0037-email-setup-and-delivery.md), [email guide](guides/email.md)): `orb add mail` asks Resend or SMTP, saves typed secrets only to `.env`, writes `internal/app/infra_mail.go` and the `.env.example` block, and prints next steps; running it again switches provider. The Resend API key and SMTP credentials are environment variables; the sender name, address and reply-to are runtime settings (`mail.*`) filled into each message by `mail.WithDefaults`. `MAIL_DELIVERY` picks Mailpit (development default) or the provider (always in production).

Added in the operations and integrations milestone ([ADR-0062](adr/0062-resend-webhooks-and-suppression-list.md)): the mail worker's sender is wrapped with `mail.WithSuppressionList`, so addresses on the suppression list (`modules/mail/suppressionpg`) get no email and their jobs are cancelled. Resend's signed webhook `POST /v1/webhooks/resend` (app module `mailevents`, on only with `RESEND_WEBHOOK_SECRET`) adds the recipients of hard bounces and complaints, once per delivery ID; operators list and remove them with `/ops/mail/suppressions`.

### 7.4 Operations APIs ([ADR-0026](adr/0026-operations-apis.md))

`/ops/*`, protected by platform roles and required 2FA. Built in the operations and upgrades milestone: runtime settings, jobs and queues with an overview, audit log with stats, release monitor, system health (`/ops/system`), retention as runtime settings enforced by a `retention` job (`/ops/retention`), maintenance mode, email and sign-in method status ([ADR-0051](adr/0051-operations-v0-5.md)). Added in the operations and integrations milestone: per-organisation setting overrides (`GET /ops/settings/{key}/overrides`, [ADR-0056](adr/0056-per-organisation-settings.md)), the email suppression list (`/ops/mail/suppressions`, [ADR-0062](adr/0062-resend-webhooks-and-suppression-list.md)), feature flags (`/ops/flags`, [ADR-0057](adr/0057-feature-flags.md)), platform service accounts and their keys (`/ops/service-accounts`, delivered by the auth module, [ADR-0058](adr/0058-api-keys-and-service-accounts.md)), live observability (`/ops/observability`, with a Server-Sent Events stream) and incidents with reports (`/ops/incidents`, [ADR-0064](adr/0064-live-observability-and-incidents.md)). API keys never carry the permissions of roles that require 2FA, so `/ops` stays human-only.

Built first in the data and identity milestone (`examples/full-single`, [ops API reference](guides/ops-api.md)): `/ops/settings` ([ADR-0031](adr/0031-runtime-settings.md)), `/ops/jobs/definitions`, `/ops/jobs/scheduled`, `/ops/jobs/runs` and `/ops/queues` ([ADR-0033](adr/0033-background-jobs.md)), `/ops/audit` ([ADR-0036](adr/0036-audit-storage.md)), `/ops/mail` ([ADR-0037](adr/0037-email-setup-and-delivery.md)). They require a signed-in user whose platform roles grant the operation's permission ([ADR-0038](adr/0038-authentication-v0-2.md)); required 2FA for ops roles arrived with strong authentication ([ADR-0043](adr/0043-two-factor-authentication.md)), with `/ops/auth/providers` ([ADR-0045](adr/0045-sign-in-provider-setup.md)).

### 7.5 API contract and docs ([ADR-0027](adr/0027-api-contract-and-docs.md))

Code-first with Huma v2, confined to `delivery/`: developers write Go input/output types and handlers; OpenAPI 3.1, validation and problem+json errors follow automatically. `my-api openapi --dir api` exports `api/openapi.json` (committed; `/ops/*` is checked for breaking changes against `api/openapi.baseline.json` with `openapi.CheckCompatible`, [ADR-0054](adr/0054-api-freeze-and-scaffold-compatibility.md)). `/docs` serves an API reference in the gorbital design, rendered from the app's own OpenAPI document by `modules/openapi/reference`, the same renderer as the public API reference ([ADR-0049](adr/0049-public-docs-and-website.md)). The Postman collection and `llms.txt` are generated from the exported spec.

### 7.6 Observability ([ADR-0007](adr/0007-observability.md), [ADR-0028](adr/0028-local-development-environment.md))

`modules/telemetry` keeps OpenTelemetry always on in the app (traces, metrics, slog logs carrying `request_id`, `trace_id` and `span_id`); export is enabled by setting `OTEL_EXPORTER_OTLP_ENDPOINT`. The Minimal preset needs no Docker. In Full apps, `orb dev` starts PostgreSQL and Mailpit, and `orb dev --observability` also starts Grafana (`grafana/otel-lgtm`). PostgreSQL always runs in Docker (the official image, through `compose.yaml` locally and a service container in CI); gorbital never downloads PostgreSQL binaries.

`orb dev` also turns on the development-only dev console APIs under `/_dev/` (`modules/devconsole`, [ADR-0065](adr/0065-local-dev-console-apis.md), [guide](guides/dev-console.md)): recent requests and log records with live streams, routes, wiring, configuration without secrets, captured email, migrations and job runs, behind a localhost `Host` check, a loopback peer check and a token `orb dev` prints for each run. They are APIs for local tools; no console UI is built.

Setting `METRICS_ADDR` also serves the same metrics in the Prometheus format on a separate listener that answers only `GET /metrics`, off by default and refused on the API's port ([ADR-0063](adr/0063-prometheus-metrics.md)); apps record Go runtime and connection pool metrics, and `telemetry.RecordRoute` around the mux labels HTTP metrics and spans with the matched route pattern.

**Live observability and incidents** (`modules/observability`, [ADR-0064](adr/0064-live-observability-and-incidents.md), [guide](guides/observability.md)). Each instance's collector middleware counts requests per minute, method and route pattern, with a latency histogram, and writes them to `observability_minutes` every 15 seconds; `/ops/observability/overview` and `/routes` add up every instance (rates, error rate, estimated percentiles, per instance and route) and `/stream` sends the overview as Server-Sent Events, rechecking the session before each event. Incidents have a severity, a status and a timeline, are audited, and have a report (JSON or Markdown) with the window's requests, audit events and releases. The `incidents_detect` job opens one automatic incident across instances when the server error rate stays above a threshold.

### 7.7 Configuration ([ADR-0020](adr/0020-constructors-and-configuration.md), [ADR-0031](adr/0031-runtime-settings.md))

Two layers. **Environment** holds secrets, credentials and infrastructure (database URL, API keys, listen addresses) and changes with a redeploy. **Runtime settings** hold non-secret tunables (expiries, limits, sender names, frontend URLs, maintenance mode): declared in Go as typed handles with defaults and bounds, stored in PostgreSQL only when changed, edited through `PUT /ops/settings/{key}` with a reason and version, recorded in history and the audit log, and applied on every instance through `LISTEN/NOTIFY`. A value is never in both layers, and secrets are never settings. Settings declared `OrgOverridable` also take a value per organisation, set by its owners and admins under `/v1/orgs/{orgId}/settings`; `Get` returns it when the context's actor acts in that organisation, and security settings (sign-in, rate limits, retention, maintenance, mail, link targets) are never overridable ([ADR-0056](adr/0056-per-organisation-settings.md)). Guide: [runtime settings](guides/runtime-settings.md).

**Feature flags** (`modules/flags`, [ADR-0057](adr/0057-feature-flags.md), [guide](guides/feature-flags.md)). Flags are declared in Go like settings, and operators change their state through `/ops/flags` with a version and a reason: enabled, a default, organisation and user allow and deny lists, and a percentage. `flag.Enabled(ctx)` reads memory and the actor: organisation lists apply to callers acting in an organisation, then user lists, then a stable SHA-256 bucket of the flag and the organisation (or user), then the default. Changes are stored with history, audited and reloaded on every instance through `LISTEN/NOTIFY`. Flags declared `Client()` are listed to signed-in clients by `GET /v1/flags` (permission `flags.flag.read`, and `GET /v1/orgs/{orgId}/flags` in multi-tenant apps). Flags aren't access control.

### 7.8 Background jobs ([ADR-0033](adr/0033-background-jobs.md))

Jobs run on PostgreSQL with River, in the API process: every instance serves HTTP and works jobs (the library allows a separate worker process, but generated apps don't include one). Each job is a **definition**, like a serverless function: developers write and deploy the code, and operators change its configuration at runtime (enabled, schedule, timeout, max attempts, queue, priority) through `/ops/jobs`, with versions, reasons, history and audit events. Schedules are 5-field cron in UTC or `@every` intervals, run once across instances by River's elected leader, and never more often than once a minute. Jobs carry the enqueuing request ID, trace and actor but never permissions, and run as the `jobs` system actor. Email is sent through `jobs.AsyncSender` with job-ID idempotency keys. River's tables are migrated by River's migrator from `cmd/migrate`. Guide: [background jobs](guides/background-jobs.md).

### 7.9 Database ([ADR-0005](adr/0005-database-strategy.md), [ADR-0028](adr/0028-local-development-environment.md), [ADR-0032](adr/0032-repository-sql.md))

PostgreSQL only, always from Docker in development, tests and CI. `modules/postgres` opens a traced pgx pool and provides `DBTX`, `InTx`, error classification, goose migrations and `pgtest` (a fresh database per test). Every pool also sets the context's organisation on its connections, which row-level security policies read (section 7.2). Repositories use hand-written SQL with one file per operation. Guide: [database](guides/database.md).

### 7.10 Idempotency keys ([ADR-0060](adr/0060-idempotency-keys.md))

Full apps accept `Idempotency-Key` on signed-in POST and PATCH requests (`modules/idempotency`). The middleware, last in the chain after authentication, claims the key per caller in PostgreSQL with one statement (so racing retries on any instance run once), fingerprints the request, and stores the final response for `idempotency.retention` (24 h) to replay it with `Idempotent-Replayed: true`. A different request with the same key gets 422, a key still in progress 409; server errors, responses setting cookies or marked `Cache-Control: no-store` (such as a new API key) and handlers calling `idempotency.DontStore` release the key. `/v1/auth/*` is excluded; the `idempotency_cleanup` job deletes expired keys. Guide: [idempotency keys](guides/idempotency.md).

---

## 8. CLI and generator ([ADR-0021](adr/0021-generator-operation-model.md))

| Command | Purpose |
|---|---|
| `orb new <name>` | Create an app (presets and prompts) |
| `orb add <feature>` | Add a feature: `mail` switches the email provider; `orgs` turns a single-tenant app multi-tenant; `rls` turns on row-level security in a multi-tenant app ([ADR-0061](adr/0061-row-level-security.md)) |
| `orb gen resource <Name> <field:type>... [--scope=user]` | One-shot layered module owned by the signed-in user or, with `--scope org`, by an organisation, with table, API and tests ([ADR-0039](adr/0039-resource-module-template.md)) |
| `orb gen job <Name> [--schedule CRON\|--every D\|--on-demand]` | Job args, worker, test and definition; config editable in `/ops/jobs` ([ADR-0033](adr/0033-background-jobs.md)) |
| `orb gen migration <name>` | Empty forward-only goose migration that runs after the existing ones |
| `orb dev [--observability] [--no-services] [--no-reload]` | Run locally with reload and Docker services; prints a dev console token per run |
| `orb upgrade [--from <version>] [--dry-run]` | Merge template changes and upgrade the library on branch `orb-upgrade/<version>`; `--major` arrives with the first v2 bridge release |
| `orb doctor` | Check the app's tools, versions, lock file, configuration and migrations |

**Foundation:** `orb new` (Minimal preset; `--module`, `--local`, `--json`, `--no-git`), `orb dev` (build, run, reload, `.env`, port check), `orb version`.

**Data and identity:** `orb gen job` (interactive or flags), `orb gen resource` (string, text and enum fields; golden-tested against `examples/full-single`'s projects module), `orb gen migration`, `orb dev` with Docker services, migrations, seed data and `--observability` ([ADR-0042](adr/0042-development-seed-data.md)), interactive `orb new`, `orb new --preset=full` (generated from `examples/full-single`, [ADR-0041](adr/0041-full-preset-generation.md)), `orb add mail`. Guide: [CLI](guides/cli.md).

**Operations and upgrades:** `gorbital.lock` v2, `orb upgrade` and `orb add orgs` ([ADR-0050](adr/0050-upgrades-and-adding-features.md)); `orb doctor`, `go run ./cmd/api openapi --dir api` exporting the Postman collection and `llms.txt`, and the operations work above ([ADR-0051](adr/0051-operations-v0-5.md)).

**Operations and integrations:** `orb add rls`, the row-level security policy in `orb gen resource --scope org` and the `row-level security` check in `orb doctor` ([ADR-0061](adr/0061-row-level-security.md)); the dev console token in `orb dev` ([ADR-0065](adr/0065-local-dev-console-apis.md)).

**Interaction ([ADR-0035](adr/0035-interactive-cli.md)):** in a terminal, commands ask for missing values with arrow-key selects, checkboxes, validated inputs and a final summary; every prompt has a flag, flags skip their prompts, and `--yes`, `--json`, `--no-input` or `CI` never prompt. Prompts and flags share validators.

- **Recipes are whole preset trees** generated from the golden apps ([ADR-0041](adr/0041-full-preset-generation.md)). API endpoints come from Go code (ADR-0027), so recipes never edit a spec file. No code runs at install time.
- **One wiring file per feature** stays a style goal: it keeps merges small.
- **Tracked vs one-shot:** `gorbital.lock` v2 records the `orb` release, the template inputs and a hash of every tracked file; `orb upgrade` rebuilds the old tree from that release, proves it against the hashes and merges 3-way ([ADR-0050](adr/0050-upgrades-and-adding-features.md)). `orb gen` output is one-shot.
- **Safety:** names validated as Go identifiers, field types from an allowlist, writes confined with `os.Root`, diff preview with risky new imports highlighted, clean git tree required.

---

## 9. Security ([ADR-0029](adr/0029-threat-model.md))

The threat model covers the framework, CLI and ecosystem, not only generated apps. Highest priorities: `gorbital.dev` domain and DNS hardening, template injection, malicious recipes, OAuth and ID-token validation, passkey origin checks, cross-tenant access, ops endpoint protection, release signing and maintainer account security. The internal security review of September 2026 found 43 issues, all fixed or accepted in writing ([report](security/2026-09-internal-review.md), [ADR-0053](adr/0053-internal-security-review.md)); the external review is open.

---

## 10. Milestones ([roadmap](roadmap.md))

`v0.1.0` is the first public release and contains every milestone marked done or built; `v1.0.0` follows the external security review ([releases](roadmap.md#releases)).

| Milestone | Status | Delivers |
|---|---|---|
| Foundation | Done, in v0.1.0 | Core library, Minimal preset, `orb new` and `orb dev`, OpenAPI docs (Scalar, since replaced by the gorbital reference), CI, signed release workflow |
| Data and identity | Done, in v0.1.0 | PostgreSQL, runtime settings, jobs, email (Resend/SMTP), email/password auth, users and roles, audit, Full preset, single-tenant, `orb dev` with Docker, seed data |
| Strong authentication | Done, in v0.1.0 | Google, Apple, TOTP, passkeys |
| Organisations | Done, in v0.1.0 | Multi-tenant organisations, tenancy prompt |
| Operations and upgrades | Done, in v0.1.0 | `gorbital.lock` v2, `orb upgrade`, `orb add orgs`, `orb doctor`, system health, audit stats, retention, maintenance mode, Postman collection and `llms.txt` |
| Stability and security review | Built, in v0.1.0; external review open | Rate limits shared across instances, internal security review, API freeze and compatibility checks, governance, documentation content with generated reference pages; open: external security review sign-off (`v1.0.0` waits for it) and maintainer actions (GitHub teams, branch protection, release environment, tag rulesets, conduct contact, domain hardening) |
| Operations and integrations | Done, in v0.1.0 | Per-organisation settings, feature flags, API keys and service accounts, GitHub sign-in, idempotency keys, row-level security option, Resend bounce and complaint webhooks with a suppression list, Prometheus `/metrics` option, live observability and incidents, local dev console APIs |
| Public website | Built early, published | Public website and docs at gorbital.dev and docs.gorbital.dev ([ADR-0049](adr/0049-public-docs-and-website.md)) |
| Dev Portal | In progress; phases built so far in v0.1.0 | A local development UI served by `orb dev` ([ADR-0066](adr/0066-dev-portal.md), [Dev Portal roadmap](dev-portal-roadmap.md)) |
| Client templates | Proposed | Docs site, dashboard and Expo app created by `orb new` from separate template repositories ([ADR-0047](adr/0047-client-templates.md)) |

---

## 11. Open items

| Item | Resolved by |
|---|---|
| ~~API contract: spec-first vs code-first~~ | Resolved: code-first with Huma ([spike](../spikes/openapi/README.md), ADR-0027) |
| ~~Unknown request fields: strict vs tolerant~~ | Resolved: tolerant (ADR-0027) |
| ~~Anchor edits: text insertion vs AST~~ | Resolved: parser-located text insertion ([spike](../spikes/anchor/README.md), ADR-0021) |
| ~~Minimal first run under 60 seconds~~ | Resolved: 12.0 s cold, 1.6 s warm in the spike; 25.0 s cold, 4.8 s warm with the real CLI at the end of the foundation milestone (`scripts/first-run.sh`) |
| ~~Scalar docs visual check in a real browser~~ | Resolved: Scalar replaced by the gorbital reference, checked in a browser in a generated app and on the site ([ADR-0049](adr/0049-public-docs-and-website.md)) |
| ~~Publish the library at `gorbital.dev`~~ | Resolved: `v0.1.0` published on 2026-09-17; `go install gorbital.dev/cli/cmd/orb@latest` |
| External security review | Open: the stability and security review work is built (in v0.1.0) and `v1.0.0` awaits a third party's sign-off; maintainer actions (GitHub teams and branch protection, `release` environment, tag rulesets, code of conduct contact) are listed in [ADR-0053](adr/0053-internal-security-review.md) |
| ~~Row-level security~~ | Resolved: optional fifth isolation layer, `orb add rls` ([ADR-0061](adr/0061-row-level-security.md)) |
| Local dev console | APIs resolved: `/_dev/` in `modules/devconsole` ([ADR-0065](adr/0065-local-dev-console-apis.md)). Open: no console UI is built; the Dev Portal in gorbital-dashboards stays on mock data |
| ~~`/ops/*` protection before authentication~~ | Resolved: sessions and platform roles replaced the interim `OPS_TOKEN` ([ADR-0038](adr/0038-authentication-v0-2.md)); ops roles require two-factor authentication ([ADR-0043](adr/0043-two-factor-authentication.md)) |
| ~~Example business module with its own repository~~ | Resolved: `examples/full-single/internal/modules/projects` owns the `projects` table with all four layers, user ownership and cross-owner tests ([ADR-0039](adr/0039-resource-module-template.md)); `orb gen resource` reproduces it exactly |
| ~~`orb new --preset=full`~~ | Resolved: templates generated from `examples/full-single`, reproduced byte for byte, with `go.mod` derived from the golden `go.mod` ([ADR-0041](adr/0041-full-preset-generation.md)) |
| ~~Audit storage~~ | Resolved: `modules/auditpg` stores events in an append-only `audit_events` table, listed by `/ops/audit` ([ADR-0036](adr/0036-audit-storage.md)) |
| ~~Email providers and setup~~ | Resolved: `modules/mail/smtp`, `modules/mail/resend` and `orb add mail` ([ADR-0037](adr/0037-email-setup-and-delivery.md)) |
| ~~Email templates and preview route~~ | Resolved: one branded layout in `mail` (`mail.Brand`, `mail.Email`) used by the modules and the app's own emails ([ADR-0078](adr/0078-branded-email-layout.md)); previews in the Dev Portal ([ADR-0074](adr/0074-dev-mail-previews-and-env-editor.md)) |
| ~~Client IP and user agent in audit events~~ | Resolved: `actor.WithClient` carries them in the context, `auth.Middleware` sets them after trusted-proxy handling, and `audit.FromContext` fills every event recorded during a request ([ADR-0036](adr/0036-audit-storage.md#security-review-fixes-2026-09-16)) |
| ~~Organisations design~~ | Resolved: [ADR-0048](adr/0048-organisations-v0-4.md) accepted and built in the organisations milestone; `orb add orgs` converts existing apps (operations and upgrades, [ADR-0050](adr/0050-upgrades-and-adding-features.md)) |
| Client templates | Open: [ADR-0047](adr/0047-client-templates.md) proposed; to decide the dashboard and docs stacks, where archives are hosted, and the bundle ID prompt before accepting |
