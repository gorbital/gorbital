# ADR-0014: Product shape, presets and creation prompts

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0001 · **Amended by:** ADR-0035 (interactive prompts with flag parity), ADR-0041 (Full preset generated from `examples/full-single`), ADR-0050 (Custom preset out of v0.5; presets are whole golden trees)

## Context

gorbital must let a developer run one command and get a working, production-grade API they own, similar in spirit to `create-next-app`, but with far more built in (authentication, database, jobs, audit, operations APIs). At the same time, security fixes must reach existing apps, and not every project wants every feature.

## Options

1. **Template only:** copy a large starter repository into the project. Fast to build; fixes never reach existing apps.
2. **Framework only:** apps import a runtime that hides everything. Upgradable; developers lose ownership and understanding.
3. **Library + CLI + recipes:** logic in versioned library modules; the CLI generates thin owned glue from recipes; presets choose which recipes apply.

## Decision

Option 3.

- **Library** `gorbital.dev/...`: all reusable and security-sensitive logic.
- **CLI** `orb`: `new`, `add`, `gen`, `dev`, `upgrade`, `doctor`.
- **Recipes:** declarative, versioned with the library they call.

`orb new <name>` offers three presets:

| Preset | Contents |
|---|---|
| **Minimal** | HTTP server, config, logging, tracing, health, security defaults, OpenAPI + `/docs`, Dockerfile. No database; Docker not required. |
| **Full** | Everything: PostgreSQL, jobs, email, authentication (ADR-0024), users/roles, tenancy choice (ADR-0023), audit, operations APIs (ADR-0026), seed data, tests, CI option. |
| **Custom** | A checklist of features. Dependencies are added automatically (for example, passkeys ⇒ auth ⇒ PostgreSQL, email, jobs). |

Prompts (each with a flag for non-interactive use: `--preset`, `--tenancy`, `--mail`, `--github`, `--yes`; interaction rules in ADR-0035):

1. Preset.
2. Tenancy, asked in business language (default single-tenant).
3. Email provider: Resend or SMTP.
4. GitHub repository and CI.

`orb new` applies `base-minimal` plus the selected feature recipes using the same engine as `orb add` (ADR-0021). The selection is stored in `gorbital.yaml`.

## Why

- The first run shows real value: signup, login and docs work immediately.
- Keeping logic in the library keeps apps secure and upgradable.
- One engine for `new` and `add` means a feature chosen at creation and the same feature added later are identical.

## Trade-offs

- Three products to version and test together.
- The Full preset is large; it is delivered over pre-release milestones (roadmap).
- Every recipe combination the Custom preset allows must be tested; the CLI restricts combinations to valid dependency sets.

## Consequences

- Golden reference apps (`examples/minimal`, `examples/full-single`, `examples/full-multi`) are hand-written and CI verifies the generator reproduces them.
- Adding a feature to Full requires a recipe, a library module or extension, docs, and upgrade tests.
