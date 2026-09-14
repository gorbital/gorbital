# apistock

**A Go application kit with production-ready foundations you own.**

> **Status: pre-alpha, architecture phase.** There is no usable code yet. This repository currently holds the design documents and architecture decision records (ADRs). Nothing described below exists until it ships in a tagged release.

## What apistock is

apistock gives Go developers the first weeks of backend foundation work (authentication, PostgreSQL, migrations, email, background jobs, audit logging, observability, CI) as idiomatic Go code that lives in **your** repository.

It is a **kit, not a framework**:

- **Plain Go.** Standard `net/http`, `log/slog`, constructors instead of a DI container, and no reflection-based wiring.
- **You own the code.** The `aps` CLI writes readable code into your project and never silently overwrites your edits.
- **Upgradeable.** Security-sensitive logic lives in versioned Go packages (`go get`). Generated glue is upgraded with a git-style 3-way merge.
- **One required dependency.** PostgreSQL. No Redis, Kafka, collector or SaaS needed to run in production.
- **No lock-in.** If you uninstall the CLI tomorrow, your app still builds, tests and deploys.

## Planned developer experience

```bash
go install apistock.dev/cli/cmd/aps@latest

aps new my-api
cd my-api
aps add postgres
aps add auth
aps gen resource Product name:text price_cents:bigint
aps dev
```

## Documentation

- [Architecture](docs/architecture.md): the full design
- [v1 scope](docs/scope-v1.md): what v1 includes, excludes and will not build
- [Architecture decision records](docs/adr/)

## Roadmap (summary)

| Phase | Focus |
|---|---|
| 0 | Architecture, ADRs, technical spikes |
| 1 | Core runtime + hand-written reference app |
| 2 | Auth, email, jobs, audit as libraries |
| 3 | `aps` CLI and code generator |
| 4 | Upgrades (`aps upgrade`) |
| 5 | `aps dev` and local dev console |
| 6 | Module SDK, community index, 1.0 |

## Contributing

The project is not accepting code contributions yet. Feedback on the architecture and ADRs is welcome through GitHub issues.

## Security

See [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE)
