# gorbital

**Production-ready Go APIs in minutes, as code you own.**

> **Status: pre-release, v1.0 in progress.** v0.1 to v0.5 are done and tagged: the core library, Minimal and Full presets, PostgreSQL, runtime settings, background jobs, audit log, email, authentication with 2FA, passkeys, Google and Apple, platform roles, multi-tenant organisations, the ops APIs, `orb upgrade` and `orb add orgs`. The library isn't published at `gorbital.dev` yet, so apps are created against a local checkout with `--local`. Docs: [docs.gorbital.dev](https://docs.gorbital.dev).

## Try it

Requires Go 1.26 or later (latest patch) and Docker. Full walkthrough with the real output: [Quickstart](docs/start/quickstart.md).

```bash
git clone https://github.com/gorbital/gorbital.git
cd gorbital/cli && go install ./cmd/orb && cd ../..   # installs orb into $(go env GOPATH)/bin
orb new my-api --preset full --tenancy single --local ./gorbital
cd my-api
git add -A && git commit -m "Create my-api"           # orb gen and orb add need a clean tree
orb dev                                               # PostgreSQL and Mailpit in Docker, migrations, seed data, the API
```

`orb dev` prints the seeded administrator's password and 2FA key once. Then:

| Open | What |
|---|---|
| http://localhost:8080/docs | Your API's reference, with "Try it" |
| http://127.0.0.1:8025 | Mailpit: every email sent in development |
| http://127.0.0.1:8080/readyz | Readiness |

If your shell says `command not found: orb`, add Go's bin directory to your `PATH` (`export PATH="$(go env GOPATH)/bin:$PATH"`) and open a new terminal. If port 5432 is taken, set `POSTGRES_PORT` and the same port in `DATABASE_URL` in `.env`. More: [Troubleshooting](docs/start/troubleshooting.md).

## What gorbital is

gorbital is a Go library plus a CLI (`orb`) that creates a complete, working API project: authentication, PostgreSQL, background jobs, email, audit logs, operations APIs, interactive docs and tests, generated as readable Go code in **your** repository.

- **Plain Go.** Standard `net/http`, `log/slog`, constructors instead of a DI container, no reflection wiring.
- **You own the code.** A layered, domain-driven structure you can read and change; the CLI never silently overwrites your edits.
- **Security in the library.** Password hashing, sessions, OAuth, 2FA and passkeys live in versioned packages, so fixes arrive with `go get`.
- **One required service.** PostgreSQL. No Redis, Kafka or SaaS needed in production.
- **No lock-in.** Uninstall the CLI and your app still builds, tests and deploys.

## Planned developer experience

```bash
go install gorbital.dev/cli/cmd/orb@latest

orb new my-api          # choose Full or Minimal; tenancy
cd my-api
orb dev                 # API at :8080, docs at /docs, local email inbox
```

| Preset | What you get |
|---|---|
| **Minimal** | HTTP server, config, logging, tracing, health checks, security defaults, OpenAPI docs. No database. |
| **Full** | Everything: PostgreSQL, jobs, email (Resend or SMTP), email/password + Google + Apple sign-in, 2FA, passkeys, users and roles, optional multi-tenant organisations, audit logs, operations APIs. |

## Documentation

- [Documentation index](docs/README.md): everything below, with what each covers
- For app builders: [What you need](docs/start/prerequisites.md), [Quickstart](docs/start/quickstart.md), [Set up sign-in](docs/sign-in/overview.md), [Every key and credential](docs/sign-in/all-keys.md), [Troubleshooting](docs/start/troubleshooting.md)
- Technical: [Architecture](docs/architecture.md), [Life of a request](docs/guides/request-lifecycle.md), [Environment variables](docs/guides/environment-variables.md), [Secrets and keys](docs/guides/secrets-and-keys.md), [Testing](docs/guides/testing.md), [Running in production](docs/guides/production.md)
- [Roadmap](docs/roadmap.md) and [architecture decision records](docs/adr/README.md)
- [Spikes](spikes/): experiments that informed decisions

## Roadmap (summary)

| Release | Status |
|---|---|
| v0.1 Foundation: core library, Minimal preset, `orb new`, `orb dev`, API docs | Done |
| v0.2 PostgreSQL, runtime settings, jobs, email, authentication, roles, audit, Full preset | Done, tagged |
| v0.3 Google and Apple sign-in, TOTP, passkeys | Done, tagged |
| v0.4 Multi-tenant organisations | Done, tagged |
| v0.5 `orb upgrade`, `orb add orgs`, `orb doctor`, system health, audit stats, retention, maintenance mode, Postman collection and `llms.txt` | Done, tagged |
| v1.0 Security review, API freeze, governance, reference docs | In progress |
| v1.1 Feature flags, per-organisation settings, API keys, GitHub login, idempotency keys, row-level security, email webhooks, Prometheus, live observability, dev console APIs | Planned |

What changed in each release: [CHANGELOG.md](CHANGELOG.md).

## Contributing

Contributions are welcome: read [CONTRIBUTING.md](CONTRIBUTING.md) first. How decisions are made: [GOVERNANCE.md](GOVERNANCE.md).

## Security

See [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE)
