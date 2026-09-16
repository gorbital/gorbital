# gorbital documentation

The documentation has two audiences, and the website at [docs.gorbital.dev](https://docs.gorbital.dev) renders both from this repository ([ADR-0049](adr/0049-public-docs-and-website.md)):

- **Guides** explain in plain words how to create, run and configure an app, click by click, assuming no prior knowledge. They live in `docs/start/` and `docs/sign-in/`.
- **Technical documentation** explains how gorbital works and why: architecture, request lifecycle, functions, configuration, security, testing and production. It lives in `docs/`.

## Start here

| If you want to | Read |
|---|---|
| Understand what gorbital is | [Introduction](start/introduction.md), [How gorbital works](start/concepts.md) |
| Install what you need | [What you need](start/prerequisites.md) |
| Create and run an app | [Quickstart](start/quickstart.md) |
| Add your own data | [Add your first resource](start/first-resource.md) |
| Set up Google, Apple, GitHub, passkeys or email | [Set up sign-in](sign-in/overview.md), [Every key and credential](sign-in/all-keys.md) |
| Fix a problem | [Troubleshooting](start/troubleshooting.md) |
| Launch | [Go-live checklist](sign-in/go-live.md), [Running in production](guides/production.md) |
| Work on gorbital itself | [Local development](guides/local-development.md), [Testing](guides/testing.md) |

## Guides for app builders

| Guide | Covers |
|---|---|
| [Introduction](start/introduction.md) | What gorbital is, the two kinds of docs |
| [What you need](start/prerequisites.md) | Go, Docker, git and the rest: what each is, why, install on macOS and Linux, how to check |
| [Quickstart](start/quickstart.md) | Install `orb`, create an app, `orb dev` with its real output, sign up, sign in as administrator, ports, running without the CLI |
| [How gorbital works](start/concepts.md) | The three parts, what an app contains, where settings live, glossary |
| [Add your first resource](start/first-resource.md) | `orb gen resource`, every generated file, the table, testing |
| [Organisations](start/organisations.md) | Multi-tenant apps |
| [Upgrading apps](start/upgrading.md) | `orb upgrade`: newer templates and library into an existing app |
| [Set up sign-in](sign-in/overview.md) | Overview of sign-in methods and where values go |
| [Every key and credential](sign-in/all-keys.md) | Every value, how it's obtained, complete development and production environments |
| [Encryption key](sign-in/encryption-key.md), [Email sending](sign-in/email.md), [Passkeys](sign-in/passkeys.md), [Passkeys in mobile apps](sign-in/passkeys-mobile.md), [Google](sign-in/google.md), [Apple](sign-in/apple.md), [GitHub](sign-in/github.md) | Step-by-step setup of each |
| [Go-live checklist](sign-in/go-live.md) | Before real users arrive |
| [Troubleshooting](start/troubleshooting.md) | Symptoms, exact messages, causes and fixes |

## Technical documentation

| Document | Covers |
|---|---|
| [Architecture](architecture.md) | Products, principles, library layout, the generated app, features, milestones |
| [Key decisions](guides/key-decisions.md) | The main design choices with alternatives, trade-offs and consequences |
| [Life of a request](guides/request-lifecycle.md) | Server, middleware in order, authentication, routing, use cases, repositories, errors |
| [Inside a generated app](guides/app-internals.md) | Every function of `internal/app` and the commands: purpose, inputs, side effects, errors |
| [Services and libraries](guides/services-and-libraries.md) | Every dependency and service: what, why, where, without it |
| [Environment variables](guides/environment-variables.md) | Every variable: reader, default, validation, secret or not |
| [Secrets and keys](guides/secrets-and-keys.md) | Every secret, token and code: creation, storage, rotation, compromise |
| [Database](guides/database.md) | `modules/postgres`, repositories, transactions, migrations, schema, relationships, indexes, pgtest |
| [Runtime settings](guides/runtime-settings.md) | `modules/settings`: environment vs runtime settings, per-organisation values |
| [Feature flags](guides/feature-flags.md) | `modules/flags`: declaring flags, targeting, percentage rollouts, `/ops/flags`, client flags |
| [Background jobs](guides/background-jobs.md) | `modules/jobs`: definitions, schedules, email, the manager |
| [Idempotency keys](guides/idempotency.md) | `modules/idempotency`: `Idempotency-Key` on POST and PATCH, replays, errors, what isn't stored |
| [Live observability and incidents](guides/observability.md) | `modules/observability`: request counts per route, `/ops/observability` and its stream, incidents, automatic detection, reports |
| [Authentication](guides/authentication.md) | Flows, sessions, 2FA, passkeys, Google, Apple and GitHub, roles, error codes |
| [Sign-in provider setup](guides/auth-providers.md) | How the app reports what's missing |
| [API keys and service accounts](guides/api-keys.md) | `gbk_` keys, scopes, personal keys, platform and organisation service accounts, what keys can't do |
| [Email](guides/email.md) | Resend or SMTP, Mailpit, sending from code, bounces, complaints and the suppression list |
| [Error handling](guides/error-handling.md) | Problem details, the mapper, codes, logging |
| [Ops API reference](guides/ops-api.md) | `/ops/*` endpoints, permissions, error codes, audit actions |
| [CLI](guides/cli.md) | Every `orb` command and flag |
| [Testing](guides/testing.md) | What's tested, helpers, commands, drift checks, CI |
| [Stability and compatibility](guides/stability.md) | What 1.0 promises not to break and the checks that enforce it: API listings, `api/surface.json`, the `/ops` baseline, `--json` schemas, scaffold compatibility, reference pages |
| [Running in production](guides/production.md) | Image, configuration, migrations, scaling, observability (including Prometheus), operations, what never to do |
| [Upgrade notes](guides/upgrade-notes.md) | What changes for existing apps in each release, and what to do before deploying |
| [Local development](guides/local-development.md) | Working on the gorbital repository |
| [Security overview](security/README.md) | How security is reviewed and reported; the [internal review of September 2026](security/2026-09-internal-review.md) |
| [Roadmap](roadmap.md) | Milestones and status |
| [Changelog](../CHANGELOG.md) | Notable changes in each release |
| [Architecture decision records](adr/README.md) | Every decision with context, options and trade-offs |

## Reference

Generated from the golden apps by `go run -C internal/tools/refdocs . -write` and checked in CI ([stability](guides/stability.md#reference-pages-docsreference)); don't edit them by hand.

| Page | Lists |
|---|---|
| [Error codes](reference/error-codes.md) | Every problem+json code: HTTP status, meaning, where it's returned |
| [Audit actions](reference/audit-actions.md) | Every audit action: when it's recorded, metadata keys |
| [Permissions and roles](reference/permissions.md) | Platform and organisation catalogs: which roles hold which permissions, required two-factor authentication |
| [Runtime settings](reference/settings.md) | Every setting: type, default, bounds, reason and restart required |
| [Jobs](reference/jobs.md) | Every job: default schedule, timeout, attempts, what it does |

## Project

| Document | Covers |
|---|---|
| [Contributing](../CONTRIBUTING.md) | Proposing a change, setting up the repository, pull requests |
| [Governance](../GOVERNANCE.md) | Roles and how decisions are made |
| [Security policy](../SECURITY.md) | Supported versions and reporting a vulnerability |
| [Code of conduct](../CODE_OF_CONDUCT.md) | How we work together |

## Brand and website

| Document | Covers |
|---|---|
| [Theme](brand/theme.md) | The visual system and voice for docs, landing page, README and CLI output |
| [Logo files](brand/logo/README.txt) | The mark, lockups, avatars and favicon |
| [Website](https://github.com/gorbital/gorbital-web) | gorbital.dev and docs.gorbital.dev: a separate Next.js repository that renders these files; `docs/docs.json` lists the pages |

## Examples

| App | Shows |
|---|---|
| [examples/minimal](../examples/minimal) | Minimal preset: HTTP, config, telemetry, health, docs; no database |
| [examples/full-single](../examples/full-single) | Full preset: PostgreSQL, runtime settings, jobs, audit log, email, authentication with 2FA, passkeys, Google and Apple, roles, ops APIs, the `projects` example |
| [examples/full-multi](../examples/full-multi) | Full preset with `--tenancy multi`: everything above plus organisations, members, invitations and org-scoped projects |

Library packages document their API in Go doc comments, rendered in the package reference on the website and by `go doc gorbital.dev/modules/jobs`.
