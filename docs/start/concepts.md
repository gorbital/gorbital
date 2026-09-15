# How apistock works

This page explains apistock in plain words: the three parts, what a new app contains, how you work with it day to day, and the terms the rest of the docs use. You don't need to know Go to follow it.

## Three parts

| Part | What it is | Where it runs |
|---|---|---|
| **`aps`**, the command-line tool | Creates your app, adds features to it and generates code, such as a new resource or a background job | Only on your computer |
| **Your app** | A Go project in your own repository: your endpoints, your database tables, your rules | Your computer while you build, then your servers |
| **The apistock library** | Go packages your app imports for the parts that are hard to get right: password hashing, sessions, passkeys, background jobs | Inside your app |

Why split it this way:

- **Your code stays yours.** Everything specific to your product is plain Go in your repository. There's no hidden framework magic to learn.
- **Security fixes are easy to get.** The risky parts are in the library, so a fix reaches your app when you update it with `go get`.
- **Nothing locks you in.** Remove `aps` and your app still builds, runs and deploys.

## What a new app contains

When you run `aps new` and choose the **Full** preset, you get a working API with:

| Feature | What it does for you |
|---|---|
| Sign-up and sign-in | Accounts with email and password, email codes, sessions, password reset, and optional authenticator apps, passkeys, Google and Apple |
| Database | PostgreSQL, with migrations that create and change your tables step by step |
| Example resource | `projects`: a complete example of data people can create, list, update and delete, to copy for your own |
| Background jobs | Work that runs later or on a schedule, such as sending email or cleaning up expired sessions |
| Email | Sign-up codes and alerts, sent through Resend or SMTP |
| Admin endpoints | `/ops/...` endpoints for administrators to change settings, run jobs and read the audit log |
| API docs | `/docs`: every endpoint with examples and a "Try it" button, generated from your code |
| Tests | Tests for every part, including the database, so you can change code with confidence |

The **Minimal** preset is a much smaller API with no database or sign-in, for when you only need a few endpoints.

## Single or multi-tenant

`aps new` asks one question that shapes your data: will different companies or teams use your app, each with their own separate data?

- **No (single-tenant):** data belongs to individual users. A to-do app is single-tenant.
- **Yes (multi-tenant):** data belongs to organisations. People join organisations, get a role and invite others. A project-management tool for companies is multi-tenant. See [Organisations](organisations.md).

## Working on your app

```bash
aps dev
```

`aps dev` is the one command you run while building. It:

1. Starts PostgreSQL and Mailpit (a local inbox that catches every email) in Docker.
2. Updates the database with any new migrations.
3. Creates example data the first time, including an administrator account.
4. Starts your API and restarts it each time you save a file.

While it runs, you have:

| Address | What's there |
|---|---|
| http://localhost:8080/docs | Your API docs, where you can try every endpoint |
| http://127.0.0.1:8025 | Mailpit: every email your app sends |

## Where settings live

Your app has three kinds of settings, and each lives in one place only.

| Kind | Examples | Where it lives | Changing it needs |
|---|---|---|---|
| Secrets and infrastructure | The database address, Google's client secret, the encryption key | Environment variables: `.env` on your computer, your host's secret settings in production | A restart |
| Runtime settings | The email sender address, how long sign-in codes stay valid | The database, changed through `/ops/settings` | Nothing: every server picks it up within moments |
| Job schedules | When a nightly cleanup runs, how often it retries | The database, changed through `/ops/jobs` | Nothing |

Secrets never go in the database, and nothing is set in two places.

## Words you'll meet

| Word | Meaning |
|---|---|
| API | The part of your product that other programs talk to over the internet, such as your website or mobile app |
| Endpoint | One address of your API that does one thing, such as `POST /v1/projects` to create a project |
| Resource | A kind of data in your API, such as projects or invoices, with endpoints to create, read, update and delete it |
| Migration | A file of SQL that changes your database's structure, applied in order |
| Environment variable | A named value your app reads when it starts, such as `DATABASE_URL` |
| Secret | A value that lets someone act as your app, such as a password or private key. It never goes in git |
| Runtime setting | A value administrators can change while the app runs, without a deploy |
| Background job | Work your app does outside a request, later or on a schedule |
| Organisation | A company or team in a multi-tenant app, with members and roles |
| Two-factor authentication (2FA) | A second step at sign-in, such as a code from an authenticator app |
| Passkey | A way to sign in with Face ID, Touch ID, Windows Hello or a phone instead of a password |
| OpenAPI | A standard description of an API; your app writes one in `api/openapi.json`, and `/docs` is built from it |
| Docker | Software that runs services such as PostgreSQL in isolated containers on your computer |

## Next

- [Quickstart](quickstart.md): create and run an app.
- [Add your first resource](first-resource.md): add your own kind of data.
- [Set up sign-in](../sign-in/overview.md): turn on Google, Apple, passkeys and email.
