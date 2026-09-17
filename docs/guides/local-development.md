# Local development

How to work on the gorbital repository: services, tests, checks and CI.

## Prerequisites

| Tool | Version | Why |
|---|---|---|
| Go | 1.26 or later (use the latest patch: Go 1.26.0 has known standard library vulnerabilities) | Build and test |
| Docker with Compose v2 | Any recent Docker Desktop or Engine | PostgreSQL for tests and examples |
| git | Any | Version control |

PostgreSQL is never installed locally or downloaded as a binary: it always runs in Docker (ADR-0028).

## Installing the CLI

```bash
cd cli && go install ./cmd/orb
orb version
```

If `orb` isn't found, add `$(go env GOPATH)/bin` to your `PATH`. Reinstall after pulling changes. See the [CLI guide](cli.md).

## The Dev Portal UI

`orb dev` serves the [Dev Portal](dev-portal.md), whose UI is built in [gorbital-dashboards](https://github.com/gorbital/gorbital-dashboards) and embedded in `orb` at build time. The built UI is committed in `cli/internal/portal/ui/dist` (`dist/BUILD` names the gorbital-dashboards commit), so a checkout, `go install` and the release binaries all serve it without Node. To embed a newer UI:

```bash
git clone https://github.com/gorbital/gorbital-dashboards.git ../gorbital-dashboards   # next to this checkout
scripts/sync-portal.sh               # needs Node 22 and pnpm 10; copies the export into cli/internal/portal/ui/dist
cd cli && go install ./cmd/orb
```

Commit `cli/internal/portal/ui/dist` when the release should ship that UI; the release workflow builds `orb` from what is committed.

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

Set `GORBITAL_POSTGRES_PORT` to use another host port.

## Mailpit

The same `compose.yaml` runs [Mailpit](https://mailpit.axllent.org), a local email inbox, for the SMTP module and example tests: SMTP on **127.0.0.1:51025**, web inbox on **http://127.0.0.1:58025** (`GORBITAL_MAILPIT_SMTP_PORT` and `GORBITAL_MAILPIT_WEB_PORT` to change). Apps send their own email to `orb dev`'s mail catcher on 127.0.0.1:1025, read in the Dev Portal ([email guide](email.md), [ADR-0074](../adr/0074-dev-mail-previews-and-env-editor.md)).

## Running tests

Database tests use `pgtest`, which reads the server URL from an environment variable; email delivery tests read Mailpit's addresses:

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://gorbital:gorbital@127.0.0.1:55432/gorbital?sslmode=disable'
export GORBITAL_TEST_MAILPIT_SMTP=127.0.0.1:51025
export GORBITAL_TEST_MAILPIT_URL=http://127.0.0.1:58025
```

| Module | Command |
|---|---|
| Core | `go test ./...` (repository root) |
| A module | `cd modules/jobs && go test -race ./...` |
| An example app | `cd examples/full-single && go test -race ./...` |
| The CLI | `cd cli && go test -race ./...` |
| The documentation | `go run -C internal/tools/docscheck .` (links, anchors, includes and `docs/docs.json`) |

What each kind of test covers, the helpers, and drift checks: [testing](testing.md).

- Without `GORBITAL_TEST_DATABASE_URL`, database tests are **skipped** with instructions.
- With `GORBITAL_REQUIRE_DB=1` (as in CI), a missing database **fails** the tests instead.
- Without the Mailpit variables, `modules/mail/smtp`'s Mailpit test is skipped and the example checks that email is queued but not delivered; `GORBITAL_REQUIRE_MAILPIT=1` (as in CI) makes the missing Mailpit a failure.
- `ORB_E2E_DOCKER=1` in `cli` runs `orb dev` in a new Full app against real Docker on free ports, signs in as the seeded administrator and checks that a registration email reaches Mailpit; its containers and volume are removed afterwards.
- `ORB_E2E=1` in `cli` runs the end-to-end tests: a generated Minimal app passes its tests, and a copy of `examples/full-single` builds after `orb add mail` switches it to SMTP and back.
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
go run ./cmd/api openapi --dir api
```

## Running the Full preset example

```bash
cd examples/full-single
orb dev      # .env, its own PostgreSQL on 127.0.0.1:5432, the mail catcher on 127.0.0.1:1025, migrations, seed data
```

The first run prints the seeded administrator's password (`admin@example.com`) once. Every run also prints a new token for the development-only `/_dev/` APIs (recent requests and logs, routes, configuration without secrets, captured email): see [dev console APIs](dev-console.md). Without the CLI:

```bash
cp .env.example .env
docker compose up -d --wait
go run ./cmd/api migrate && go run ./cmd/api seed
go run ./cmd/api
```

The app reads `.env` only through `orb dev`; with plain `go run`, export the variables first (for example `set -a; . ./.env; set +a`). The dev console APIs stay off unless you also export `DEV_CONSOLE_TOKEN` (at least 32 characters, such as `openssl rand -base64 32`); never put it in `.env`.

## CI

`.github/workflows/ci.yml` runs, per module: gofmt, `go vet`, `go test -race` against PostgreSQL and Mailpit service containers, golangci-lint, govulncheck, OpenAPI drift checks for the example apps, recipe drift for the Minimal, Full and multi-tenant Full presets, the API listings (`internal/tools/apicheck`), the reference and Methods pages (`internal/tools/refdocs`), the documentation's links, includes and navigation (`internal/tools/docscheck`), end-to-end generated-app tests, the scaffold compatibility check, and a gitleaks secret scan.

The workflows are currently **disabled on GitHub** during active development. Re-enable them with:

```bash
gh workflow enable CI -R gorbital/gorbital
gh workflow enable "Release orb" -R gorbital/gorbital
```

## Commits

Commit messages use a short imperative subject and a bullet body describing what changed. Don't add co-author trailers.
