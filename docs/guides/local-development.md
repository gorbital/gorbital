# Local development

How to work on the apistock repository: services, tests, checks and CI.

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | 1.26 or later (use the latest patch: Go 1.26.0 has known standard library vulnerabilities) | Build and test |
| Docker with Compose v2 | Any recent Docker Desktop or Engine | PostgreSQL for tests and examples |
| git | Any | Version control |

PostgreSQL is never installed locally or downloaded as a binary: it always runs in Docker (ADR-0028).

## Installing the CLI

```bash
cd cli && go install ./cmd/aps
aps version
```

If `aps` isn't found, add `$(go env GOPATH)/bin` to your `PATH`. Reinstall after pulling changes. See the [CLI guide](cli.md).

## Repository layout

Each directory with a `go.mod` is its own Go module: the core library at the root, `modules/*`, `cli`, and each `examples/*` app. Run Go commands inside the module you are working on; `replace` directives point modules at the local checkout.

## PostgreSQL

The root `compose.yaml` runs `postgres:18` for module tests on **127.0.0.1:55432** (not 5432, so it can run next to other projects).

```bash
docker compose up -d --wait          # start
docker compose ps                    # check it is healthy
docker compose down                  # stop, keep data
docker compose down -v               # stop and delete data (also removes old pgtest templates)
```

Set `APISTOCK_POSTGRES_PORT` to use another host port.

## Running tests

Database tests use `pgtest`, which reads the server URL from an environment variable:

```bash
export APISTOCK_TEST_DATABASE_URL='postgres://apistock:apistock@127.0.0.1:55432/apistock?sslmode=disable'
```

| Module | Command |
|---|---|
| Core | `go test ./...` (repository root) |
| A module | `cd modules/jobs && go test -race ./...` |
| An example app | `cd examples/full-single && go test -race ./...` |

- Without `APISTOCK_TEST_DATABASE_URL`, database tests are **skipped** with instructions.
- With `APISTOCK_REQUIRE_DB=1` (as in CI), a missing database **fails** the tests instead.
- Every test gets its own database, cloned from a migrated template, and dropped afterwards; tests are isolated and can run in parallel across packages.

## Checks

Run these in the module you changed before committing:

```bash
gofmt -l .
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run --config ../../.golangci.yml ./...
GOTOOLCHAIN=go1.26.8 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

Adjust the `--config` path to reach the repository root's `.golangci.yml`. For an example app, also regenerate its API contract after changing endpoints:

```bash
go run ./cmd/api openapi > api/openapi.json
```

## Running the Full preset example

```bash
cd examples/full-single
cp .env.example .env                 # set OPS_TOKEN: openssl rand -hex 32
docker compose up -d --wait          # its own PostgreSQL on 127.0.0.1:5432 (POSTGRES_PORT to change)
go run ./cmd/migrate
go run ./cmd/api
```

The app reads `.env` only through `aps dev`; with plain `go run`, export the variables first (for example `set -a; . ./.env; set +a`).

## CI

`.github/workflows/ci.yml` runs, per module: gofmt, `go vet`, `go test -race` against a PostgreSQL service container, golangci-lint, govulncheck, OpenAPI drift checks for the example apps, recipe drift for the Minimal preset, an end-to-end generated-app test, and a gitleaks secret scan.

The workflows are currently **disabled on GitHub** during active development. Re-enable them with:

```bash
gh workflow enable CI -R apistockhq/apistock
gh workflow enable "Release aps" -R apistockhq/apistock
```

## Commits

Commit messages use a short imperative subject and a bullet body describing what changed. Don't add co-author trailers.
