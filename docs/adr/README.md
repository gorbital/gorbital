# Architecture Decision Records

Each ADR records one decision: context, options, decision, reasons, trade-offs and consequences.

- ADRs are never deleted. When a decision changes, a new ADR supersedes the old one and the old ADR's status says so.
- Status values: **Proposed**, **Accepted**, **Superseded**, **Rejected**.
- Architecture v2 (2026-09-14) is summarised in [../architecture.md](../architecture.md).

## Index

| ADR | Title | Status |
|---|---|---|
| [0001](0001-framework-boundaries.md) | Framework boundaries | Accepted, amended by 0014, 0019 |
| [0002](0002-module-architecture.md) | Module architecture | Superseded by 0019, 0021 |
| [0003](0003-code-generation.md) | Code generation | Accepted, amended by 0021 |
| [0004](0004-dependency-injection.md) | Dependency injection | Superseded by 0017, 0020 |
| [0005](0005-database-strategy.md) | Database strategy | Accepted, amended by 0032, 0033 |
| [0006](0006-authentication.md) | Authentication | Superseded by 0024 |
| [0007](0007-observability.md) | Observability | Accepted, amended by 0019, 0028, 0053, 0063 |
| [0008](0008-configuration.md) | Configuration | Superseded by 0020 |
| [0009](0009-repository-strategy.md) | Repository strategy | Accepted, amended by 0019 |
| [0010](0010-dashboard-architecture.md) | Dashboard architecture | Accepted, amended by 0026, 0028, 0066 |
| [0011](0011-github-integration.md) | GitHub integration | Accepted |
| [0012](0012-versioning-and-upgrades.md) | Versioning and upgrades | Superseded by 0015, 0016 |
| [0013](0013-multi-tenancy.md) | Multi-tenancy | Superseded by 0023 |
| [0014](0014-product-shape-and-presets.md) | Product shape, presets and creation prompts | Accepted, amended by 0035, 0041, 0050 |
| [0015](0015-public-api-and-stability-tiers.md) | Public API surface and stability tiers | Accepted, amended by 0054 |
| [0016](0016-scaffold-compatibility-and-upgrades.md) | Scaffold compatibility and upgrade path | Accepted, amended by 0050, 0054 |
| [0017](0017-application-lifecycle.md) | Application lifecycle | Accepted, amended by 0053 |
| [0018](0018-error-contract.md) | Error contract and problem+json | Accepted |
| [0019](0019-module-dependency-rules.md) | Module dependency rules and core budget | Accepted, amended by 0033 |
| [0020](0020-constructors-and-configuration.md) | Constructors and configuration | Accepted, amended by 0031, 0053 |
| [0021](0021-generator-operation-model.md) | Generator operation model | Accepted, amended by 0041, 0050, 0066 |
| [0022](0022-generated-application-layout.md) | Generated application layout | Accepted, amended by 0032 |
| [0023](0023-tenancy.md) | Tenancy | Accepted, amended by 0033, 0048, 0061 |
| [0024](0024-authentication-methods.md) | Authentication methods | Accepted, amended by 0038, 0043, 0044, 0046, 0058, 0059 |
| [0025](0025-email-providers.md) | Email providers | Accepted, amended by 0033, 0037, 0062, 0078 |
| [0026](0026-operations-apis.md) | Operations APIs | Accepted, amended by 0031, 0033, 0034, 0036, 0038, 0051, 0064 |
| [0027](0027-api-contract-and-docs.md) | API contract and documentation | Accepted, amended by 0049, 0051, 0053 |
| [0028](0028-local-development-environment.md) | Local development environment | Accepted, amended by 0042, 0065, 0066 |
| [0029](0029-threat-model.md) | Threat model: framework, CLI and ecosystem | Accepted, amended by 0036, 0038, 0053, 0056, 0057, 0058, 0059, 0060, 0061, 0062, 0063, 0064, 0065, 0066 |
| [0030](0030-context-and-correlation.md) | Context and correlation propagation | Accepted, amended by 0053 |
| [0031](0031-runtime-settings.md) | Runtime settings | Accepted, amended by 0056 |
| [0032](0032-repository-sql.md) | Hand-written SQL in repositories | Accepted |
| [0033](0033-background-jobs.md) | Background jobs | Accepted, amended by 0053 |
| [0034](0034-interim-ops-token.md) | Interim ops token | Superseded by 0038 |
| [0035](0035-interactive-cli.md) | Interactive CLI with flag parity | Accepted, amended by 0037 |
| [0036](0036-audit-storage.md) | Audit storage | Accepted, amended by 0053 |
| [0037](0037-email-setup-and-delivery.md) | Email setup and delivery | Accepted, amended by 0053, 0062 |
| [0038](0038-authentication-v0-2.md) | Authentication in v0.2 | Accepted, amended by 0048, 0053, 0058, 0078 |
| [0039](0039-resource-module-template.md) | Resource module template | Accepted, amended by 0048, 0058 |
| [0040](0040-release-tracking.md) | Release tracking | Accepted |
| [0041](0041-full-preset-generation.md) | Full preset generation | Accepted, amended by 0050, 0053 |
| [0042](0042-development-seed-data.md) | Development seed data | Accepted |
| [0043](0043-two-factor-authentication.md) | Two-factor authentication | Accepted, amended by 0044, 0046, 0053, 0058 |
| [0044](0044-passkeys.md) | Passkeys | Accepted |
| [0045](0045-sign-in-provider-setup.md) | Sign-in provider setup | Accepted, amended by 0046, 0059 |
| [0046](0046-google-and-apple-sign-in.md) | Google and Apple sign-in | Accepted, amended by 0053, 0059 |
| [0047](0047-client-templates.md) | Client templates: docs site, dashboard and mobile apps from separate template repositories | Proposed |
| [0048](0048-organisations-v0-4.md) | Organisations (v0.4): tables, org roles, requests, invitations, personal workspaces, lifecycle, generation | Accepted, amended by 0050, 0053, 0056, 0058, 0061, 0078 |
| [0049](0049-public-docs-and-website.md) | Public website: landing page, framework docs and API reference in the gorbital look; the Go generator in `site/` was replaced by the gorbital-web Next.js repository on 2026-09-15; generated reference pages and the changelog page added 2026-09-16 | Accepted |
| [0050](0050-upgrades-and-adding-features.md) | Upgrading apps and adding features to them (v0.5): lock v2, rebuilt merge base, `orb upgrade`, `orb add orgs` | Accepted, amended by 0053, 0061 |
| [0051](0051-operations-v0-5.md) | Operations in v0.5: audit stats, system health, jobs overview, retention, maintenance mode, API exports, `orb doctor` | Accepted, amended by 0053, 0064 |
| [0052](0052-shared-rate-limits.md) | Shared rate limits and trusted proxies | Accepted, amended by 0053 |
| [0053](0053-internal-security-review.md) | Internal security review before the external one: six areas, severity scale, every finding fixed with a regression test or accepted in writing; report in `docs/security/` | Accepted |
| [0054](0054-api-freeze-and-scaffold-compatibility.md) | API freeze: stability markers, API listings, public-surface inventory, `/ops` baseline, `--json` schema version, scaffold compatibility check | Accepted |
| [0055](0055-governance-and-contribution.md) | Governance and contribution: roles, decisions, reviews, supported versions, changelog | Accepted |
| [0056](0056-per-organisation-settings.md) | Per-organisation settings: org-overridable declarations, organisation values resolved from the context, organisation and `/ops` endpoints | Accepted |
| [0057](0057-feature-flags.md) | Feature flags: flags declared in code, organisation and user targeting, stable percentage rollouts, `/ops/flags` with reasons, history and audit, client flags | Accepted |
| [0058](0058-api-keys-and-service-accounts.md) | API keys and service accounts: `gbk_` keys stored as hashes with expiry, scopes and last use, platform and organisation service accounts, never 2FA-required permissions | Accepted |
| [0059](0059-github-sign-in.md) | GitHub sign-in: OAuth without OpenID Connect, verified primary email, never authoritative, links bound to the browser and session, `AUTH_DEFAULT_RETURN_TO` | Accepted |
| [0060](0060-idempotency-keys.md) | Idempotency keys: `modules/idempotency`, stored and replayed POST and PATCH responses per caller | Accepted |
| [0061](0061-row-level-security.md) | Row-level security option: every connection carries its organisation, `orb add rls` forces policies on organisation tables, audited bypass for system paths | Accepted |
| [0062](0062-resend-webhooks-and-suppression-list.md) | Resend bounce and complaint webhooks and the email suppression list | Accepted |
| [0063](0063-prometheus-metrics.md) | Prometheus metrics endpoint: exporter on a separate `METRICS_ADDR` listener, Go runtime and connection pool metrics, route labels through request copies | Accepted |
| [0064](0064-live-observability-and-incidents.md) | Live observability and incidents: `modules/observability` request minutes shared through PostgreSQL, `/ops/observability` with a live stream, incidents with timelines, automatic detection and reports | Accepted |
| [0065](0065-local-dev-console-apis.md) | Local dev console APIs: development-only `/_dev/` endpoints in `modules/devconsole` behind Host, loopback and per-run token checks, request and log buffers with streams, configuration without secrets, `orb dev` token | Accepted, amended by 0066 |
| [0066](0066-dev-portal.md) | Dev Portal: `orb dev` serves the embedded portal UI, a `/_portal/api/` for the app's state, output, restarts and generator plans, and a proxy to the app; per-run token, Host and loopback checks, a write header; generators plan before they write (`genplan`) | Accepted, amended by 0067 |
| [0067](0067-table-editor-and-pgmeta.md) | Table Editor and `pgmeta`: `orb dev` reads the catalog and edits rows through `cli/internal/pgmeta` (identifiers from the catalog, values as text parameters, tables without a key read-only), and every schema change is a plan rendered as a goose migration that the app's `cmd/migrate` applies; ownership classes user, managed, system | Accepted, amended by 0068, 0069 |
| [0068](0068-sql-editor.md) | SQL editor: scripts run through the simple protocol in one transaction rolled back by default, results as text cells, warnings before running, EXPLAIN in a rolled-back transaction, snippets as files under `db/queries`, history and favourites under `.orb/portal`, a script saved as a migration | Accepted |
| [0069](0069-schema-visualiser-objects-and-migrations.md) | Schema visualiser, database objects and migrations: `postgres.MigrateDown` and `MigrationList`, `cmd/migrate --down` and `--redo` in development, plan kinds for extensions, functions, triggers, views and enums, `GET /_portal/api/db/migrations` and the portal's roll-back and redo actions | Accepted, amended by 0080 |
| [0079](0079-hourly-log-archive.md) | Hourly log archive: `gorbital.dev/modules/storage/logarchive` tees the app's logger into a spool file per hour under `LOG_ARCHIVE_DIR` and stores each finished hour gzipped in the app's file storage as `logs/<service>/<YYYY>/<MM>/<DD>/<HH>[.<instance>].jsonl.gz` (partial hours at shutdown or when switched off), on only while the `logs.archive.enabled` runtime setting is; failed uploads retried, never on the logging path | Accepted |
| [0080](0080-live-schema-status.md) | Live schema status: `orb dev` publishes a `schema` event (`source` startup, code, migrate, portal or sql; `applied`, `pending` with `new` or `out_of_order`, `edited`, `needs_restart`, `problem`) after every migrate run, on every change to `db/migrations` (watched even with `--no-reload`, with a console warning when it won't be applied), and after a committed DDL in the SQL editor; `GET /_portal/api/db/schema-status`; applied files' content recorded in `.orb/portal/migrations.json` to find files edited after they were applied | Accepted |
| [0081](0081-a-framework-you-import.md) | A framework you import: a composition library `gorbital.dev/gorbital` above the modules orders what `internal/app` builds today; additive to v0.1 (v0.1 apps build against v0.2); one explicit line per built-in in `main.go`; `LoadConfig` → `New` → `Run` with `Main`; principle 3 revised | Accepted |
| [0082](0082-routes-guards-and-middleware.md) | Routes, guards and middleware: generic `gorbital.Get/Post/…` over router groups in `Module.Routes`; deny by default with `guard.Public()`; guards (`Permission`, `Role`, `RecentReauth`, `RateLimit`, `Idempotent`, `OrgMember`, `New`) as operation middleware before input parsing; `httpx.Middleware` at app, module, group and route level; `WithStack`; route annotations and file-based routing not in v0.2 | Accepted |
| [0083](0083-modules-stack-migrations-and-ejection.md) | Modules, the default stack, migrations and ejection: `gorbital.Module` and `Deps`; built-in modules in `gorbital.dev/gorbital/…http`; app layout with generated `internal/modules/modules.gen.go`; the named default stack; `Authenticator`; one goose history with a frozen version table for library migrations; `orb eject` | Accepted |
| [0084](0084-versioned-documentation.md) | Versioned documentation: `release/v0.1` frozen and served at `/v0.1/`, the latest unprefixed, the unreleased line at `/next/`, from git refs; generated Methods pages checked in CI; Examples as runnable apps included by marker | Accepted |
| [0085](0085-security-layers.md) | Security layers: `httpx/timeout` (503 `request_timeout` from a timer sharing the writer's lock, streams untouched), `httpx/ipfilter` on the address after trusted proxies (403 `ip_not_allowed`), both packages of their own so v0.1 apps' recorded surfaces don't change, `gorbital.dev/webhook` verifiers with `guard.Webhook` before input parsing, `modules/jwt` for external identity providers (JWKS caching and rate-limited refresh, 401 `invalid_token`); threat model per layer | Accepted |
| [0086](0086-dev-portal-tunnel.md) | Dev Portal tunnel: `orb dev --tunnel quick\|named` and the Tunnel screen run the developer's own `cloudflared` (never downloaded) in its own process group, development only; the named tunnel's token only in cloudflared's environment (`TUNNEL_TOKEN`), redacted everywhere; only the app is tunnelled, and the dev console and dev operator also refuse requests with forwarding headers; `.env` proposals through the env editor and provider addresses from the app's routes | Accepted |
| [0087](0087-testing-sign-in-from-the-dev-portal.md) | Testing sign-in from the Dev Portal: dev console extensions (`AuthSetup.DevEndpoints`) serve `/_dev/auth/test/`; offline checks per method, network checks (clock, client credentials with a made-up code), live round trips through the real provider to the real callback, intercepted before sign-in with in-memory single-use state, verified as sign-in does and reported to the portal's result page; passkey ceremony on `APP_PUBLIC_URL` with a ticket; throwaway TOTP secret; test email; nothing written, no audit events | Accepted |
| [0077](0077-generators-hub-first-run-and-project-settings.md) | The generators hub runs `orb add mail`, `storage` and `rls` through plans with a diff (`orgs` through its dry run and branch workflow); `orb new` starts `orb dev` and opens the portal in a terminal (`--no-start`); Project Settings from the manifest and `.env` with a danger zone (`reset-database`, clear logs, inbox, SQL history) | Accepted |
| [0078](0078-branded-email-layout.md) | Branded email layout: `mail.Brand` renders a `mail.Email` (preheader, title, paragraphs, a one-time code block, a button, closing lines) as HTML tables with inline styles and as text; `auth.NewBrandedEmails`, `orgs.NewBrandedEmails` and `auth.BrandedEmailPreviews` take the brand; Full apps build it in `internal/app/mail.go` | Accepted |
| [0076](0076-git-screen.md) | The Dev Portal's Git screen runs the developer's `git` through `orb dev` (`/_portal/api/git/…`): status, diffs, staging by file and hunk, commits, branches, fetch, pull and push (never force), merge preview with `merge-tree`, conflicts opened in the editor, the log graph; no history rewriting | Accepted |
| [0075](0075-file-storage.md) | File storage: `gorbital.dev/modules/storage` (`Store` with `local` and `s3` drivers for S3, Spaces, R2 and MinIO through the MinIO client), wired into the Full apps (`STORAGE_*`), operators' API `/ops/storage…` behind `ops.storage.read` and `ops.storage.write`, `orb add storage`, the portal's Storage screen with a production guard | Accepted |
| [0074](0074-dev-mail-previews-and-env-editor.md) | Dev mail: an SMTP catcher inside `orb dev` (`MAIL_DELIVERY=devmail`, the development default; Mailpit removed from the compose template) with the inbox at `/_portal/api/mail…`; email previews from the real builders (`auth.EmailPreviews`, `/_dev/mail/previews`, the console's first POST); the `.env` editor at `/_portal/api/env` | Accepted, amended by 0078 |
| [0073](0073-observability-screen.md) | The Dev Portal's Observability screen on what exists (request minutes, `/ops/system`, jobs, audit) plus `pgmeta` statistics (`pg_stat_activity`, locks, sizes, `pg_stat_statements` preloaded by the development compose, index advice), a `gopsutil` sampler for the machine and the app process, and a health table across services; no OTLP receiver in `orb` for now | Accepted |
| [0072](0072-local-log-store.md) | The Dev Portal's log store: `orb dev` keeps the app's records as JSON Lines segments under `.orb/portal/logs` (8 MiB segments, 64 MiB in all), parsed from the app's JSON output (`APP_LOG_FORMAT`, set to json by `orb dev`, rendered as text for the terminal), `orb dev`'s messages and the PostgreSQL container; `httpx.AccessLog` adds `path`, `source` and the authenticated `user_id` through an `AccessNote`; endpoints for queries, histogram, live tail, error groups and saved filters | Accepted |
| [0071](0071-job-kinds-and-ejection.md) | Jobs from the portal: `orb gen job --kind custom\|http\|sql\|email\|dispatch` renders one `Work` per kind; `//orb:job {json}` marker on the definition with the worker file's hash; a worker edited by hand is ejected (a custom job edited in code); `jobDeps` gains `pool`, `mailer`, `httpClient`, `runJob`; `GET /_portal/api/jobs` | Accepted |
| [0070](0070-operators-account-apis.md) | Operators' account APIs: `/ops/auth/users…` in the auth module behind `ops.auth.read` and a new `ops.auth.write`; bans (`banned_at`, every sign-in refused, sessions and keys revoked); impersonation only with the dev console; rate limiter list and reset (`ratelimitpg.Reset`) | Accepted |
