# ADR-0028: Local development environment

**Status:** Accepted (2026-09-14) · **Amends:** ADR-0007, ADR-0010 · **Amended by:** ADR-0042 (seed password printed once, never stored), ADR-0065 (dev console APIs)

## Context

The product promise is a working API on the first try: `orb new`, then `orb dev`. Full apps need PostgreSQL and a safe email inbox; developers benefit from seeing traces and logs; first-run time and laptop resources matter. Minimal apps must run without Docker.

## Options

1. Developers install and configure PostgreSQL, a mail catcher and observability tools themselves.
2. Embedded binaries downloaded by the CLI (embedded PostgreSQL).
3. Docker Compose managed by `orb dev`, with heavier tools opt-in.

## Decision

Option 3.

| Command | Minimal preset | Full preset |
|---|---|---|
| `orb dev` | Build, run, reload on change; no Docker | Docker Compose: PostgreSQL + Mailpit; migrations applied; seed data on first run; build, run, reload |
| `orb dev --observability` | Adds Grafana (`grafana/otel-lgtm`) | Adds Grafana (`grafana/otel-lgtm`) |

On start, `orb dev` prints:

```text
✓ API        http://localhost:8080
✓ API docs   http://localhost:8080/docs
✓ Emails     http://localhost:8025
  Google login: not configured → docs/auth-providers.md
  Tip: orb dev --observability to see traces and logs
```

| Rule | Decision |
|---|---|
| Docker missing (Full) | Stop with a clear message: install Docker, or set `DATABASE_URL` to an existing PostgreSQL |
| Ports | Checked before start; conflicts reported with the process name where available |
| Email | Always delivered to Mailpit in development (ADR-0025) |
| Telemetry | The app always emits OpenTelemetry; exporting to Grafana only with `--observability` |
| Default admin | Created by seed on first run; the random password is printed once and never stored ([ADR-0042](0042-development-seed-data.md)) |
| Services | Defined in the app's owned `compose.yaml`; `orb dev` never uses hidden containers |
| Without the CLI | `docker compose up -d` plus `go run ./cmd/api` must work |
| Custom dev console | v1.1: development-only `/_dev/` APIs with a per-run token printed by `orb dev` ([ADR-0065](0065-local-dev-console-apis.md)); Grafana stays the traces and metrics viewer |

## Why

Docker Compose gives production-like PostgreSQL with no manual setup; making Grafana opt-in keeps the first run fast and light.

## Trade-offs

- Full apps require Docker for the zero-setup path.
- Observability isn't visible until the developer opts in.

## Consequences

- [First-run spike](../../spikes/firstrun/README.md): Minimal from clean caches in 12.0 s (build CLI, `orb new`, build, `/docs` ready), 1.6 s with warm caches. Target under 60 seconds met.
- Full preset timing (including Docker image pulls) is measured in v0.2 and documented.

## PostgreSQL always runs in Docker (2026-09-14)

Development, tests and CI all get PostgreSQL from a Docker container. gorbital never downloads or embeds PostgreSQL binaries and never requires a locally installed `postgres` or `psql`.

| Context | Where PostgreSQL comes from |
|---|---|
| Generated app, `orb dev` | Service `postgres` in the app's owned `compose.yaml`, started by `orb dev` (or `docker compose up -d`) |
| Generated app tests (`repository/`, `test/e2e`) | The same Compose database; `pgtest` creates a throwaway database per test package from a migrated template |
| gorbital repository (module tests, examples) | `compose.yaml` at the repository root; `docker compose up -d --wait` |
| CI | The same official `postgres` image as a GitHub Actions service container |

| Rule | Decision |
|---|---|
| Image | Official `postgres` image, pinned to one major version in every `compose.yaml` and CI; upgraded deliberately |
| Binding | Host ports bound to `127.0.0.1` only (threat 11) |
| Host port | Configurable in `.env`; `orb dev` checks it like `APP_ADDR` |
| Data | Named volume per app, so `docker compose down` keeps data and `down -v` resets it |
| Health | Compose healthcheck with `pg_isready`; `orb dev` waits for healthy before migrating |
| Test connection | `pgtest` reads `GORBITAL_TEST_DATABASE_URL`; when it is unset the test is skipped with the exact `docker compose up` command, and CI sets `GORBITAL_REQUIRE_DB=1` so a missing database fails instead of skipping |
| Migrations and seed | Run by the app's Go commands (`cmd/migrate`, `cmd/seed`), never by `psql` |
| Rejected | Embedded PostgreSQL downloads (option 2 above) and testcontainers (a Docker API client dependency in every app, and hidden containers the developer can't see in `compose.yaml`) |

## v0.1 implementation notes

- `orb dev` loads `.env` into the app's environment; variables already set in the real environment win. The app itself has no dotenv dependency.
- Before starting, `orb dev` checks that `APP_ADDR` (default `127.0.0.1:8080`) is free and, if not, stops with a message suggesting another `APP_ADDR`.
- Reload polls watched files (Go sources, module files, `.env`, `.html`, `.json`, `.sql`) every 500 ms. A failed build keeps the previous version running. The app runs in its own process group so Ctrl+C stops it exactly once.
- Measured with the real CLI (`scripts/first-run.sh`): 25.0 s from clean caches (196 MB of modules, mostly OpenTelemetry exporter dependencies), 4.8 s warm. Target met.

## v0.2 implementation notes (2026-09-15)

- `orb dev` reads `features` in `gorbital.yaml`. Apps with `postgres` take the Docker path; Minimal apps still build and run without Docker unless `--observability` is given.
- Before the first start: `.env` is created from `.env.example` (mode 0600) when missing; `docker compose version` and `docker compose ps --services --status running` check Docker; the host ports of services that aren't already running are checked (`POSTGRES_PORT`, `MAILPIT_SMTP_PORT`, `MAILPIT_WEB_PORT`, and with `--observability` `GRAFANA_PORT` and `OTLP_HTTP_PORT`), a taken port naming the `.env` line that moves it; `docker compose up -d --wait`; `go run ./cmd/migrate`; `go run ./cmd/seed` when the app has it ([ADR-0042](0042-development-seed-data.md)); then the banner.
- `--no-services` skips Docker and uses the addresses in `.env`; migrations and seed still run. Without Docker, the error offers that path.
- Since ADR-0043, `orb dev` also fills an empty `AUTH_ENCRYPTION_KEYS` in `.env` with `dev:<random 32-byte key>` (and keeps the file at mode 0600), only for apps whose `.env.example` declares the variable and when the environment doesn't set it, so seed data can enroll the administrator in two-factor authentication.
- While running, a changed or new `.sql` file under `db/migrations` runs migrations after a successful build and before the restart; a failed migration keeps the previous version running.
- Services are left running when `orb dev` stops, so restarts are fast; `docker compose down` stops them.
- `--observability`: Grafana is the `grafana/otel-lgtm:0.33.0` service behind the `observability` Compose profile in each preset's owned `compose.yaml` (the Minimal preset gains a `compose.yaml` holding only it), bound to 127.0.0.1. `orb dev` sets `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:<OTLP_HTTP_PORT>` for the app process only, over any `.env` value.
- The banner's "Google login: not configured" line arrives with social login (v0.3).
- Tests: the order of commands, `.env` creation, port conflicts, missing Docker and `--no-services` run with fake commands; `ORB_E2E_DOCKER=1` runs `orb dev` in a new Full app against real Docker, signs in as the seeded administrator and finds a registration email in Mailpit.
- Measured with that test (2026-09-15, Docker Desktop on macOS, images and Go caches warm): the API answers `/readyz` 9.6 s after `orb dev` starts, including Compose health waits, migrations and seed data. Runs that pull images depend on the network and aren't measured.
