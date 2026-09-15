# apistock documentation

| Document | What it covers |
|---|---|
| [Architecture](architecture.md) | The design overview: products, principles, library layout, generated app, features |
| [Roadmap](roadmap.md) | Milestones, what each delivers, current status |
| [Architecture decision records](adr/README.md) | Every decision with its context, options and trade-offs |
| [Theme](brand/theme.md) | The visual system and voice for docs, landing page, README and CLI output: name, colour, type, layout, the mark, CLI output grammar |

## Guides

| Guide | For |
|---|---|
| [CLI](guides/cli.md) | Installing `aps`; interactive prompts and flags for `aps new`, `aps gen job`, `aps add mail` and `aps dev`; exit codes |
| [Local development](guides/local-development.md) | Prerequisites, Docker PostgreSQL, running tests, lint and vulnerability checks, CI |
| [Database](guides/database.md) | `modules/postgres`: connecting, repositories with hand-written SQL, transactions, migrations, `pgtest` |
| [Runtime settings](guides/runtime-settings.md) | `modules/settings`: environment vs runtime settings, declaring, reading and changing settings |
| [Background jobs](guides/background-jobs.md) | `modules/jobs`: job definitions, schedules, runtime configuration, enqueueing, email, the manager |
| [Authentication](guides/authentication.md) | Sign-up and sign-in (cookie or bearer token), your first administrator, roles, sessions, settings, where the code lives, error codes |
| [Email](guides/email.md) | Choosing Resend or SMTP with `aps add mail`, where each setting lives, Mailpit, sending from code, troubleshooting |
| [Ops API reference](guides/ops-api.md) | `/ops/settings`, `/ops/jobs/*`, `/ops/queues`, `/ops/audit`, `/ops/mail`: authentication, endpoints, error codes, audit actions |

## Examples

| App | Shows |
|---|---|
| [examples/minimal](../examples/minimal) | Minimal preset: HTTP, config, telemetry, health, docs; no database |
| [examples/full-single](../examples/full-single) | Full preset (in progress): PostgreSQL, runtime settings, jobs, audit log, email, authentication and roles, and the ops APIs |

Library packages also document their API in Go doc comments (`go doc apistock.dev/modules/jobs`).
