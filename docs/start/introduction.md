# Introduction

gorbital builds the backend of your app: the API your website or mobile app talks to. One command creates a complete Go project with sign-in, a database, email and background jobs already working. The code lands in your own repository, and every file is yours to read and change.

> [!NOTE]
> gorbital is pre-release. The library isn't published at `gorbital.dev` yet, so apps are created from a copy of the repository on your computer. Everything these docs describe is built and tested.

## Two kinds of docs

| You want to | Read |
|---|---|
| Create an app, run it, add your own data, set up Google or Apple sign-in, go live | **Guides**: step by step, written for people who haven't done it before |
| Understand how it's built: the architecture, every package and function, and why each decision was made | **Technical**: architecture, subsystems, the package reference and the decision records |

## What you get

- **Sign-in:** email and password, codes by email, authenticator apps, passkeys, Google, Apple and GitHub, and API keys for scripts and services.
- **A database:** PostgreSQL, with your tables and migrations in your repository.
- **Email:** Resend or any SMTP server, with a local inbox while you develop.
- **Background jobs:** work that runs later or on a schedule, such as cleanups and emails.
- **Organisations**, if different companies or teams will use your app with their own separate data, with optional row-level security in PostgreSQL.
- **Admin endpoints** to change settings and feature flags and run jobs without a deploy, watch live request and error rates, record incidents, and an audit log of who did what.
- **Safe retries:** clients can send an `Idempotency-Key` so a repeated request doesn't run twice.
- **API docs** at `/docs` in every app, generated from your code, and development-only APIs under `/_dev/` showing recent requests, logs and configuration while you build.

## What it decides for you

gorbital makes the early decisions so you can start building. Each one comes with its cost.

| Decided | What it costs you |
|---|---|
| PostgreSQL is the only database | No MySQL or SQLite. If you need those, gorbital isn't for you. |
| Queries are plain SQL, one file each | More files than an ORM, but every query is readable in a code review. |
| Security code lives in a library | Fixes reach your app with `go get`; the sign-in flows in your repository are yours to maintain. |
| You choose single or multi-tenant at the start | Moving from single to multi-tenant later is one command (`orb add orgs`) that merges into your edits; the other way round isn't supported. |

Every decision has a written record with the alternatives considered: see the [decision records](../adr/README.md).

## Where to go next

- [What you need](prerequisites.md): Go, Docker and git, and how to check each works.
- [Quickstart](quickstart.md): create an app and run it on your computer.
- [How gorbital works](concepts.md): the parts and the words you'll meet, in plain terms.
- [Set up sign-in](../sign-in/overview.md): every key for Google, Apple, passkeys and email, click by click.
- [Troubleshooting](troubleshooting.md): exact error messages and their fixes.
- [Architecture overview](../architecture.md): how the library, the CLI and a generated app fit together.
