# apistock documentation

The documentation has two audiences, and the website at [docs.apistock.dev](https://docs.apistock.dev) renders both from this repository ([ADR-0049](adr/0049-public-docs-and-website.md)):

- **Guides** explain in plain words how to create, run and configure an app, click by click, assuming no prior knowledge. They live in `site/content/`.
- **Technical documentation** explains how apistock works and why: architecture, request lifecycle, functions, configuration, security, testing and production. It lives in `docs/`.

## Start here

| If you want to | Read |
|---|---|
| Understand what apistock is | [Introduction](../site/content/introduction.md), [How apistock works](../site/content/concepts.md) |
| Install what you need | [What you need](../site/content/prerequisites.md) |
| Create and run an app | [Quickstart](../site/content/quickstart.md) |
| Add your own data | [Add your first resource](../site/content/first-resource.md) |
| Set up Google, Apple, passkeys or email | [Set up sign-in](../site/content/sign-in/overview.md), [Every key and credential](../site/content/sign-in/all-keys.md) |
| Fix a problem | [Troubleshooting](../site/content/troubleshooting.md) |
| Launch | [Go-live checklist](../site/content/sign-in/go-live.md), [Running in production](guides/production.md) |
| Work on apistock itself | [Local development](guides/local-development.md), [Testing](guides/testing.md) |

## Guides for app builders

| Guide | Covers |
|---|---|
| [Introduction](../site/content/introduction.md) | What apistock is, the two kinds of docs |
| [What you need](../site/content/prerequisites.md) | Go, Docker, git and the rest: what each is, why, install on macOS and Linux, how to check |
| [Quickstart](../site/content/quickstart.md) | Install `aps`, create an app, `aps dev` with its real output, sign up, sign in as administrator, ports, running without the CLI |
| [How apistock works](../site/content/concepts.md) | The three parts, what an app contains, where settings live, glossary |
| [Add your first resource](../site/content/first-resource.md) | `aps gen resource`, every generated file, the table, testing |
| [Organisations](../site/content/organisations.md) | Multi-tenant apps |
| [Set up sign-in](../site/content/sign-in/overview.md) | Overview of sign-in methods and where values go |
| [Every key and credential](../site/content/sign-in/all-keys.md) | Every value, how it's obtained, complete development and production environments |
| [Encryption key](../site/content/sign-in/encryption-key.md), [Email sending](../site/content/sign-in/email.md), [Passkeys](../site/content/sign-in/passkeys.md), [Passkeys in mobile apps](../site/content/sign-in/passkeys-mobile.md), [Google](../site/content/sign-in/google.md), [Apple](../site/content/sign-in/apple.md) | Step-by-step setup of each |
| [Go-live checklist](../site/content/sign-in/go-live.md) | Before real users arrive |
| [Troubleshooting](../site/content/troubleshooting.md) | Symptoms, exact messages, causes and fixes |

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
| [Runtime settings](guides/runtime-settings.md) | `modules/settings`: environment vs runtime settings |
| [Background jobs](guides/background-jobs.md) | `modules/jobs`: definitions, schedules, email, the manager |
| [Authentication](guides/authentication.md) | Flows, sessions, 2FA, passkeys, Google and Apple, roles, error codes |
| [Sign-in provider setup](guides/auth-providers.md) | How the app reports what's missing |
| [Email](guides/email.md) | Resend or SMTP, Mailpit, sending from code |
| [Error handling](guides/error-handling.md) | Problem details, the mapper, codes, logging |
| [Ops API reference](guides/ops-api.md) | `/ops/*` endpoints, permissions, error codes, audit actions |
| [CLI](guides/cli.md) | Every `aps` command and flag |
| [Testing](guides/testing.md) | What's tested, helpers, commands, drift checks, CI |
| [Running in production](guides/production.md) | Image, configuration, migrations, scaling, observability, operations, what never to do |
| [Local development](guides/local-development.md) | Working on the apistock repository |
| [Roadmap](roadmap.md) | Milestones and status |
| [Architecture decision records](adr/README.md) | Every decision with context, options and trade-offs |

## Brand and website

| Document | Covers |
|---|---|
| [Theme](brand/theme.md) | The visual system and voice for docs, landing page, README and CLI output |
| [Logo files](brand/logo/README.txt) | The mark, lockups, avatars and favicon |
| [Website](../site/README.md) | How apistock.dev and docs.apistock.dev are built, run locally and published |

## Examples

| App | Shows |
|---|---|
| [examples/minimal](../examples/minimal) | Minimal preset: HTTP, config, telemetry, health, docs; no database |
| [examples/full-single](../examples/full-single) | Full preset: PostgreSQL, runtime settings, jobs, audit log, email, authentication with 2FA, passkeys, Google and Apple, roles, ops APIs, the `projects` example |
| [examples/full-multi](../examples/full-multi) | Full preset with `--tenancy multi`: everything above plus organisations, members, invitations and org-scoped projects |

Library packages document their API in Go doc comments, rendered in the package reference on the website and by `go doc apistock.dev/modules/jobs`.
