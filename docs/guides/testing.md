# Testing

How gorbital and generated apps are tested: what runs, what it needs, and how to run it so that passing means something.

## Principles

- **Real PostgreSQL, never mocks for SQL.** Repository and use case tests run against PostgreSQL in Docker, because constraint names, locking, transactions and `CHECK` rules are part of the behaviour ([ADR-0028](../adr/0028-local-development-environment.md), [ADR-0032](../adr/0032-repository-sql.md)).
- **A database per test.** `pgtest` clones a migrated template database for each test and drops it afterwards, so tests are isolated and run in parallel.
- **Skipped isn't passed.** Database and Mailpit tests skip, with instructions, when their services aren't configured. Set `GORBITAL_REQUIRE_DB=1` and `GORBITAL_REQUIRE_MAILPIT=1` to turn skips into failures before claiming a run is green.
- **Fakes for third parties, not for us.** Google and Apple are replaced by `socialtest`, a local OpenID provider with real signatures; passkeys by `passkeytest`, a software authenticator. The code under test is the production code.
- **Generated code is tested as generated.** Golden apps are the templates; the CLI proves it reproduces them byte for byte, and generates new resources and apps whose own test suites must pass.

## Running tests in a generated app

With `orb dev` running (or `docker compose up -d --wait`):

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://acme-api:acme-api@127.0.0.1:5432/acme-api?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test ./...
```

The URL names your development server, but tests never touch your development database: `pgtest` creates `pgtest_…` databases next to it from a template migrated with `db/migrations`, and drops them.

Add Mailpit to test email delivery end to end:

```bash
export GORBITAL_TEST_MAILPIT_SMTP=127.0.0.1:1025
export GORBITAL_TEST_MAILPIT_URL=http://127.0.0.1:8025
```

| Package | What its tests cover | Needs |
|---|---|---|
| `internal/modules/<m>/domain` | Rules and validation | Nothing |
| `internal/modules/<m>/repository` | Every SQL operation, constraint mapping, ordering and pagination | PostgreSQL |
| `internal/modules/<m>/usecase` | Flows, authorization, ownership, audit events, transactions | PostgreSQL |
| `internal/app` | Whole-app HTTP tests through `App.Handler()` with `httptest`: sign-up and email codes, sessions, 2FA, passkeys (`passkeytest`), Google and Apple (`socialtest`), ops endpoints, settings across two app instances, jobs through `/ops/jobs`, seed data, OpenAPI export, docs, configuration errors | PostgreSQL; Mailpit for delivery checks |
| `internal/app/surface_test.go` | Error codes, audit actions, permissions, roles, settings, jobs and feature flags match `api/surface.json`: nothing recorded may disappear, and new names must be recorded with `go test ./internal/app -run TestPublicSurface -update` ([stability](stability.md)) | Nothing |
| `internal/app/api_compat_test.go` | `/ops/*` doesn't break clients of `api/openapi.baseline.json` | Nothing |
| `internal/app/architecture_test.go` | Layer import rules: `domain` imports only the standard library, `delivery` never imports `repository`, modules don't import each other, only `internal/app` reads the environment | Nothing |

In multi-tenant apps, `internal/app` tests connect the app as `gorbital_app_test`, a role without `BYPASSRLS` the tests create, so row-level security policies apply to them once `orb add rls` has run. `GORBITAL_TEST_RLS=1 go test ./internal/app` runs them with the policies before that ([row-level security](row-level-security.md#testing)).

Useful flags: `go test -race ./...` (CI always uses it), `go test -run TestProjectsEndToEnd ./internal/app`, `go test -count=1` to bypass the cache after changing migrations.

## Test helpers

### `pgtest`

```go
pool := pgtest.New(t, pgtest.WithMigrations(migrations.FS))
```

| Function | Returns |
|---|---|
| `pgtest.New(t, opts...)` | A `*pgxpool.Pool` on a fresh database |
| `pgtest.NewDatabase(t, opts...)` | The URL of a fresh database, for tests that start the whole app from config |
| `pgtest.URL(t)` | The server URL itself |

`WithMigrations(fsys)` migrates a template once per distinct set of files; each test's database is `CREATE DATABASE … TEMPLATE …`, which takes milliseconds. `WithMaxConns(n)` defaults to 4. Stale templates from old migration sets are removed by `docker compose down -v`.

### `passkeytest`

`gorbital.dev/modules/auth/passkey/passkeytest` creates and signs WebAuthn attestation and assertion responses like a real authenticator, so registration, passwordless sign-in, passkeys as a second factor and clone detection run in `go test`.

### `socialtest`

`gorbital.dev/modules/auth/social/socialtest` runs an `httptest` OpenID provider with discovery, JWKS, authorization, token and revoke endpoints, and signs ID tokens and Apple notifications. Apps point Google and Apple at it through `providerEndpoints` in tests.

## Running tests in the gorbital repository

Each directory with a `go.mod` is its own module. Start the repository's services (ports 55432, 51025 and 58025, so they don't collide with apps):

```bash
docker compose up -d --wait
export GORBITAL_TEST_DATABASE_URL='postgres://gorbital:gorbital@127.0.0.1:55432/gorbital?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
export GORBITAL_TEST_MAILPIT_SMTP=127.0.0.1:51025
export GORBITAL_TEST_MAILPIT_URL=http://127.0.0.1:58025
export GORBITAL_REQUIRE_MAILPIT=1
```

| Module | Command | Notes |
|---|---|---|
| Core | `go test -race ./...` at the root | Includes `internal/archtest`, the core dependency budget |
| A library module | `cd modules/auth && go test -race ./...` | |
| A golden app | `cd examples/full-single && go test -race ./...` | Also `full-multi` and `minimal` |
| CLI | `cd cli && go test -race ./...` | Recipes, generators, `orb add`, `orb upgrade` merges |
| API listings | `go run -C internal/tools/apicheck .` at the root | Exported Go API of every library module against `api/*.txt`; `-write` records additions ([stability](stability.md)) |
| CLI end to end | `cd cli && ORB_E2E=1 go test ./...` | Generates Minimal and Full apps, runs their suites; `orb add mail` round trip |
| Scaffold compatibility | `cd cli && ORB_COMPAT=1 go test -run TestScaffoldCompatibility ./internal/cli/` | Apps generated by the latest `v1.*` release build and pass their tests against this library; skips without a v1 tag; `ORB_COMPAT_FROM=<tag>` checks another release |
| `orb dev` with Docker | `cd cli && ORB_E2E_DOCKER=1 go test ./...` | Runs `orb dev` in a new Full app on free ports, signs in as the seeded administrator, checks email reaches Mailpit, removes its containers |
| Website | `cd site && go test ./...` | Builds both sites and fails on broken links |

Each module has its own list; to run everything, loop over the `go.mod` files:

```bash
for m in $(git ls-files '*go.mod' | xargs -n1 dirname); do (cd "$m" && go test -race ./...) || break; done
```

## Drift checks

Generated artifacts are committed, and tests fail when they're stale:

| Artifact | Regenerate with | Checked by |
|---|---|---|
| CLI templates in `cli/internal/recipes` | `cd cli && go generate ./internal/recipes` | `cli/internal/recipes` tests compare them with `examples/*` |
| `examples/*/api/openapi.json` | `go run ./cmd/api openapi > api/openapi.json` in the app | CI's OpenAPI drift job |
| `full-multi` vs `full-single` | Edit both | A drift test keeps them identical outside the files organisations change |
| `docs/reference/*.md` | `go run -C internal/tools/refdocs . -write` (needs the test database) | `go run -C internal/tools/refdocs .` in CI ([Stability](stability.md#reference-pages-docsreference)) |
| Generated resources | `orb gen resource` | CLI tests generate `projects` and compare it with each golden app's module |
| `api/*.txt` (exported Go API) | `go run -C internal/tools/apicheck . -write` | CI's API listing step; missing lines are breaking changes |
| `examples/full-*/api/surface.json` | `go test ./internal/app -run TestPublicSurface -update` in the app | `TestPublicSurface`; removals fail even after regenerating the file, in review |
| `cli/internal/cli/testdata/json` | `cd cli && go test ./internal/cli -run TestJSONOutputs -update` | `TestJSONOutputs` |

## Checks before committing

In each module you changed:

```bash
gofmt -l .
go vet ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run --config <repo>/.golangci.yml ./...
GOTOOLCHAIN=go1.26.8 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

## CI

`.github/workflows/ci.yml` runs the jobs above on Go 1.26 and 1.27 with PostgreSQL and Mailpit service containers and `GORBITAL_REQUIRE_DB=1`: tests with `-race` for every module, golangci-lint, recipe and OpenAPI drift, API listings, reference pages, end-to-end generation, govulncheck, the scaffold compatibility check and gitleaks. Library tags run `gorelease` in `release-library.yml`. The workflows are currently disabled on GitHub during active development, so run the commands locally ([local development](local-development.md#ci)).
