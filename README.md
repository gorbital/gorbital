# apistock

**Production-ready Go APIs in minutes, as code you own.**

> **Status: pre-alpha.** v0.1 (core library + Minimal preset) is implemented and tested but not released. v0.2 is in progress: PostgreSQL, runtime settings, background jobs, audit storage, email (Resend or SMTP, chosen with `aps add mail`), authentication with platform roles, and their admin APIs are implemented in the library and in the [Full preset example](examples/full-single); `aps new --preset=full` is next. The library isn't published at `apistock.dev` yet, so apps are created against a local checkout with `--local`.

## Try v0.1 from a checkout

```bash
git clone git@github.com:apistockhq/apistock.git
cd apistock/cli && go install ./cmd/aps && cd ../..   # installs aps into $(go env GOPATH)/bin
aps new my-api --local ./apistock                     # or just `aps new` to be asked step by step
cd my-api && aps dev        # set APP_ADDR in .env to use a port other than 8080
```

Requires Go 1.26 or later. If your shell says `command not found: aps`, add Go's bin directory to your `PATH` (`export PATH="$(go env GOPATH)/bin:$PATH"`) and open a new terminal. Every command can be answered interactively with arrow keys or driven entirely by flags; see the [CLI guide](docs/guides/cli.md).

## Try the Full preset example (v0.2, in progress)

Requires Docker. PostgreSQL always runs in a container.

```bash
cd apistock/examples/full-single
cp .env.example .env
docker compose up -d --wait
set -a; . ./.env; set +a
go run ./cmd/migrate
go run ./cmd/api                     # docs at http://127.0.0.1:8080/docs
```

It shows sign-up and sign-in (`/v1/auth`; make yourself an admin with `go run ./cmd/api grant-role you@example.com platform_admin`), runtime settings (`/ops/settings`), Lambda-style background jobs (`/ops/jobs`), the audit log (`/ops/audit`) and email (`/ops/mail`, with every development email in Mailpit at http://127.0.0.1:8025). Choose Resend or SMTP with `aps add mail`. See the [ops API reference](docs/guides/ops-api.md) and the [email guide](docs/guides/email.md).

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

aps new my-api          # choose Full, Minimal or Custom; tenancy; email provider
cd my-api
aps dev                 # API at :8080, docs at /docs, local email inbox
```

| Preset | What you get |
|---|---|
| **Minimal** | HTTP server, config, logging, tracing, health checks, security defaults, OpenAPI docs. No database. |
| **Full** | Everything: PostgreSQL, jobs, email (Resend or SMTP), email/password + Google + Apple sign-in, 2FA, passkeys, users and roles, optional multi-tenant organisations, audit logs, operations APIs. |
| **Custom** | Pick features from a list. |

## Documentation

- [Documentation index](docs/README.md)
- [Architecture](docs/architecture.md): the design overview
- [Roadmap](docs/roadmap.md): milestones and what each one delivers
- [Architecture decision records](docs/adr/README.md)
- Guides: [CLI](docs/guides/cli.md), [local development](docs/guides/local-development.md), [database](docs/guides/database.md), [runtime settings](docs/guides/runtime-settings.md), [background jobs](docs/guides/background-jobs.md), [ops API reference](docs/guides/ops-api.md)
- [Spikes](spikes/): experiments that informed decisions

## Roadmap (summary)

| Release | Focus |
|---|---|
| v0.1 | Foundation: core library, Minimal preset, `aps new`, `aps dev`, API docs |
| v0.2 (in progress) | PostgreSQL, runtime settings, Lambda-style jobs, email, authentication, users and roles, audit, Full and Custom presets |
| v0.3 | Google and Apple sign-in, TOTP, passkeys |
| v0.4 | Multi-tenant organisations |
| v0.5 | Operations APIs, Postman, `aps upgrade` |
| v1.0 | External security review, stable API |

## Contributing

The project is not accepting code contributions yet. Feedback on the architecture and ADRs is welcome through GitHub issues.

## Security

See [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE)
