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
| [0007](0007-observability.md) | Observability | Accepted, amended by 0019, 0028 |
| [0008](0008-configuration.md) | Configuration | Superseded by 0020 |
| [0009](0009-repository-strategy.md) | Repository strategy | Accepted, amended by 0019 |
| [0010](0010-dashboard-architecture.md) | Dashboard architecture | Accepted, amended by 0026, 0028 |
| [0011](0011-github-integration.md) | GitHub integration | Accepted |
| [0012](0012-versioning-and-upgrades.md) | Versioning and upgrades | Superseded by 0015, 0016 |
| [0013](0013-multi-tenancy.md) | Multi-tenancy | Superseded by 0023 |
| [0014](0014-product-shape-and-presets.md) | Product shape, presets and creation prompts | Accepted, amended by 0035, 0041, 0050 |
| [0015](0015-public-api-and-stability-tiers.md) | Public API surface and stability tiers | Accepted |
| [0016](0016-scaffold-compatibility-and-upgrades.md) | Scaffold compatibility and upgrade path | Accepted, amended by 0050 |
| [0017](0017-application-lifecycle.md) | Application lifecycle | Accepted |
| [0018](0018-error-contract.md) | Error contract and problem+json | Accepted |
| [0019](0019-module-dependency-rules.md) | Module dependency rules and core budget | Accepted, amended by 0033 |
| [0020](0020-constructors-and-configuration.md) | Constructors and configuration | Accepted, amended by 0031 |
| [0021](0021-generator-operation-model.md) | Generator operation model | Accepted, amended by 0041, 0050 |
| [0022](0022-generated-application-layout.md) | Generated application layout | Accepted, amended by 0032 |
| [0023](0023-tenancy.md) | Tenancy | Accepted, amended by 0033, 0048 |
| [0024](0024-authentication-methods.md) | Authentication methods | Accepted, amended by 0038, 0043, 0044, 0046 |
| [0025](0025-email-providers.md) | Email providers | Accepted, amended by 0033, 0037 |
| [0026](0026-operations-apis.md) | Operations APIs | Accepted, amended by 0031, 0033, 0034, 0036, 0038, 0051 |
| [0027](0027-api-contract-and-docs.md) | API contract and documentation | Accepted, amended by 0049, 0051 |
| [0028](0028-local-development-environment.md) | Local development environment | Accepted |
| [0029](0029-threat-model.md) | Threat model: framework, CLI and ecosystem | Accepted, amended by 0036, 0038 |
| [0030](0030-context-and-correlation.md) | Context and correlation propagation | Accepted |
| [0031](0031-runtime-settings.md) | Runtime settings | Accepted |
| [0032](0032-repository-sql.md) | Hand-written SQL in repositories | Accepted |
| [0033](0033-background-jobs.md) | Background jobs | Accepted |
| [0034](0034-interim-ops-token.md) | Interim ops token | Superseded by 0038 |
| [0035](0035-interactive-cli.md) | Interactive CLI with flag parity | Accepted, amended by 0037 |
| [0036](0036-audit-storage.md) | Audit storage | Accepted |
| [0037](0037-email-setup-and-delivery.md) | Email setup and delivery | Accepted |
| [0038](0038-authentication-v0-2.md) | Authentication in v0.2 | Accepted, amended by 0048 |
| [0039](0039-resource-module-template.md) | Resource module template | Accepted, amended by 0048 |
| [0040](0040-release-tracking.md) | Release tracking | Accepted |
| [0041](0041-full-preset-generation.md) | Full preset generation | Accepted, amended by 0050 |
| [0042](0042-development-seed-data.md) | Development seed data | Accepted |
| [0043](0043-two-factor-authentication.md) | Two-factor authentication | Accepted, amended by 0044, 0046 |
| [0044](0044-passkeys.md) | Passkeys | Accepted |
| [0045](0045-sign-in-provider-setup.md) | Sign-in provider setup | Accepted, amended by 0046 |
| [0046](0046-google-and-apple-sign-in.md) | Google and Apple sign-in | Accepted |
| [0047](0047-client-templates.md) | Client templates: docs site, dashboard and mobile apps from separate template repositories | Proposed |
| [0048](0048-organisations-v0-4.md) | Organisations (v0.4): tables, org roles, requests, invitations, personal workspaces, lifecycle, generation | Accepted, amended by 0050 |
| [0049](0049-public-docs-and-website.md) | Public website: landing page, framework docs and API reference in the gorbital look; the Go generator in `site/` was replaced by the gorbital-web Next.js repository on 2026-09-15 | Accepted |
| [0050](0050-upgrades-and-adding-features.md) | Upgrading apps and adding features to them (v0.5): lock v2, rebuilt merge base, `orb upgrade`, `orb add orgs` | Accepted |
| [0051](0051-operations-v0-5.md) | Operations in v0.5: audit stats, system health, jobs overview, retention, maintenance mode, API exports, `orb doctor` | Accepted |
| [0052](0052-shared-rate-limits.md) | Shared rate limits and trusted proxies | Accepted |
| [0055](0055-governance-and-contribution.md) | Governance and contribution: roles, decisions, reviews, supported versions, changelog | Accepted |
