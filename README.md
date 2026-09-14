# apistock

**Production-ready Go APIs in minutes, as code you own.**

> **Status: pre-alpha, architecture phase.** There is no usable code yet. This repository holds the architecture, decision records (ADRs) and technical spikes. Nothing described below exists until it ships in a tagged release.

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

- [Architecture](docs/architecture.md): the design overview
- [Roadmap](docs/roadmap.md): milestones and what each one delivers
- [Architecture decision records](docs/adr/README.md)
- [Spikes](spikes/): experiments that informed decisions

## Roadmap (summary)

| Release | Focus |
|---|---|
| v0.1 | Foundation: core library, Minimal preset, `aps new`, `aps dev`, API docs |
| v0.2 | PostgreSQL, jobs, email, authentication, users and roles, audit, Full and Custom presets |
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
