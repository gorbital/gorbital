# Introduction

apistock is a Go library and a CLI, `aps`, that write a production-ready API into your repository: PostgreSQL, sign-in with two-factor authentication and passkeys, organisations, background jobs, email, an audit log and operations endpoints. The generated code is yours to read and change. The parts that need security fixes live in versioned library modules, so fixes reach you with `go get`.

> [!NOTE]
> apistock is pre-release. The library isn't published at `apistock.dev` yet, so apps are created from a checkout of the repository. Everything these docs describe is implemented and tested in the golden apps under [`examples/`](../../examples).

## What it decides for you

Opinions are the product. Each one comes with its cost, and with a record of why.

| Decided | Cost |
|---|---|
| PostgreSQL only, no ORM | No MySQL, no SQLite. If that rules you out, it rules you out. |
| Hand-written SQL, one file per operation | More files than a query builder. Each is a query you can read in review. |
| Authentication is a library; the flows are in your app | Security fixes arrive with `go get`, and you maintain the flows you change. |
| Tenancy is chosen when you create the app | Moving from single to multi-tenant with a command waits for `aps add orgs` in v0.5. |
| Runtime settings live in PostgreSQL | Change them without a deploy. Secrets stay in the environment. |

Every decision has its context, options and trade-offs written down: see the [decision records](../../docs/adr/README.md).

## Presets

| Preset | What you get | Needs |
|---|---|---|
| **Minimal** | An HTTP API with configuration, telemetry, health checks, security headers and interactive docs | Go |
| **Full** | Everything in Minimal, plus PostgreSQL, runtime settings, background jobs, email, authentication with platform roles, audit log, release tracking, `/ops/*` APIs and example code | Go and Docker |

A Full app is single-tenant by default: records belong to users. With `--tenancy multi`, records belong to [organisations](organisations.md), with members, roles and invitations.

## Where to go next

- [Quickstart](quickstart.md): create a Full app and run it locally.
- [Architecture](../../docs/architecture.md): how the library, the CLI and a generated app fit together.
- [CLI reference](../../docs/guides/cli.md): every command, question and flag.
- [API reference](../../examples/full-multi/api/openapi.json): every endpoint of the example multi-tenant app.
