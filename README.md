# apistock

**Production-ready Go APIs in minutes, as code you own.**

> **Status: pre-alpha.** v0.1 to v0.4 are done, and v0.2, v0.3 and v0.4 are tagged: the core library, Minimal and Full presets, PostgreSQL, runtime settings, background jobs, audit log, email, authentication with 2FA, passkeys, Google and Apple, platform roles, multi-tenant organisations and the ops APIs. v0.5 is in progress: `apistock.lock` v2, `aps upgrade` and `aps add orgs` are done. The library isn't published at `apistock.dev` yet, so apps are created against a local checkout with `--local`. Docs: [docs.apistock.dev](https://docs.apistock.dev).

## Try it

Requires Go 1.26 or later (latest patch) and Docker. Full walkthrough with the real output: [Quickstart](site/content/quickstart.md).

```bash
git clone https://github.com/apistockhq/apistock.git
cd apistock/cli && go install ./cmd/aps && cd ../..   # installs aps into $(go env GOPATH)/bin
aps new my-api --preset full --tenancy single --local ./apistock
cd my-api
git add -A && git commit -m "Create my-api"           # aps gen and aps add need a clean tree
aps dev                                               # PostgreSQL and Mailpit in Docker, migrations, seed data, the API
```

`aps dev` prints the seeded administrator's password and 2FA key once. Then:

| Open | What |
|---|---|
| http://localhost:8080/docs | Your API's reference, with "Try it" |
| http://127.0.0.1:8025 | Mailpit: every email sent in development |
| http://127.0.0.1:8080/readyz | Readiness |

If your shell says `command not found: aps`, add Go's bin directory to your `PATH` (`export PATH="$(go env GOPATH)/bin:$PATH"`) and open a new terminal. If port 5432 is taken, set `POSTGRES_PORT` and the same port in `DATABASE_URL` in `.env`. More: [Troubleshooting](site/content/troubleshooting.md).

## What apistock is

apistock is a Go library plus a CLI (`aps`) that creates a complete, working API project: authentication, PostgreSQL, background jobs, email, audit logs, operations APIs, interactive docs and tests, generated as readable Go code in **your** repository.

- **Plain Go.** Standard `net/http`, `log/slog`, constructors instead of a DI container, no reflection wiring.
- **You own the code.** A layered, domain-driven structure you can read and change; the CLI never silently overwrites your edits.
- **Security in the library.** Password hashing, sessions, OAuth, 2FA and passkeys live in versioned packages, so fixes arrive with `go get`.
- **One required service.** PostgreSQL. No Redis, Kafka or SaaS needed in production.
- **No lock-in.** Uninstall the CLI and your app still builds, tests and deploys.

## Planned developer experience

```bash
go install apistock.dev/cli/cmd/aps@latest

aps new my-api          # choose Full or Minimal; tenancy
cd my-api
aps dev                 # API at :8080, docs at /docs, local email inbox
```

| Preset | What you get |
|---|---|
| **Minimal** | HTTP server, config, logging, tracing, health checks, security defaults, OpenAPI docs. No database. |
| **Full** | Everything: PostgreSQL, jobs, email (Resend or SMTP), email/password + Google + Apple sign-in, 2FA, passkeys, users and roles, optional multi-tenant organisations, audit logs, operations APIs. |

## Documentation

- [Documentation index](docs/README.md): everything below, with what each covers
- For app builders: [What you need](site/content/prerequisites.md), [Quickstart](site/content/quickstart.md), [Set up sign-in](site/content/sign-in/overview.md), [Every key and credential](site/content/sign-in/all-keys.md), [Troubleshooting](site/content/troubleshooting.md)
- Technical: [Architecture](docs/architecture.md), [Life of a request](docs/guides/request-lifecycle.md), [Environment variables](docs/guides/environment-variables.md), [Secrets and keys](docs/guides/secrets-and-keys.md), [Testing](docs/guides/testing.md), [Running in production](docs/guides/production.md)
- [Roadmap](docs/roadmap.md) and [architecture decision records](docs/adr/README.md)
- [Spikes](spikes/): experiments that informed decisions

## Roadmap (summary)

| Release | Focus |
|---|---|
| Release | Status |
|---|---|
| v0.1 Foundation: core library, Minimal preset, `aps new`, `aps dev`, API docs | Done |
| v0.2 PostgreSQL, runtime settings, jobs, email, authentication, roles, audit, Full preset | Done, tagged |
| v0.3 Google and Apple sign-in, TOTP, passkeys | Done, tagged |
| v0.4 Multi-tenant organisations | Done, tagged |
| v0.5 `aps upgrade`, `aps add orgs`, `aps doctor`, system health, audit stats, retention, maintenance mode, Postman collection and `llms.txt` | Done |
| v1.0 External security review, stable API | Planned |

## Contributing

The project is not accepting code contributions yet. Feedback on the architecture and ADRs is welcome through GitHub issues.

## Security

See [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE)
