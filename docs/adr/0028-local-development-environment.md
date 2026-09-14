# ADR-0028: Local development environment

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0007, ADR-0010

## Context

The product promise is a working API on the first try: `aps new`, then `aps dev`. Full apps need PostgreSQL and a safe email inbox; developers benefit from seeing traces and logs; first-run time and laptop resources matter. Minimal apps must run without Docker.

## Options

1. Developers install and configure PostgreSQL, a mail catcher and observability tools themselves.
2. Embedded binaries downloaded by the CLI (embedded PostgreSQL).
3. Docker Compose managed by `aps dev`, with heavier tools opt-in.

## Decision

Option 3.

| Command | Minimal preset | Full preset |
|---|---|---|
| `aps dev` | Build, run, reload on change; no Docker | Docker Compose: PostgreSQL + Mailpit; migrations applied; seed data on first run; build, run, reload |
| `aps dev --observability` | Adds Grafana (`grafana/otel-lgtm`) | Adds Grafana (`grafana/otel-lgtm`) |

On start, `aps dev` prints:

```text
✓ API        http://localhost:8080
✓ API docs   http://localhost:8080/docs
✓ Emails     http://localhost:8025
  Google login: not configured → docs/auth-providers.md
  Tip: aps dev --observability to see traces and logs
```

| Rule | Decision |
|---|---|
| Docker missing (Full) | Stop with a clear message: install Docker, or set `DATABASE_URL` to an existing PostgreSQL |
| Ports | Checked before start; conflicts reported with the process name where available |
| Email | Always delivered to Mailpit in development (ADR-0025) |
| Telemetry | The app always emits OpenTelemetry; exporting to Grafana only with `--observability` |
| Default admin | Created by seed on first run; credentials printed once and stored in `.env` |
| Services | Defined in the app's owned `compose.yaml`; `aps dev` never uses hidden containers |
| Without the CLI | `docker compose up -d` plus `go run ./cmd/api` must work |
| Custom dev console | v1.1; may replace Grafana for local viewing |

## Why

Docker Compose gives production-like PostgreSQL with no manual setup; making Grafana opt-in keeps the first run fast and light.

## Trade-offs

- Full apps require Docker for the zero-setup path.
- Observability isn't visible until the developer opts in.

## Consequences

- [First-run spike](../../spikes/firstrun/README.md): Minimal from clean caches in 12.0 s (build CLI, `aps new`, build, `/docs` ready), 1.6 s with warm caches. Target under 60 seconds met.
- Full preset timing (including Docker image pulls) is measured in v0.2 and documented.
