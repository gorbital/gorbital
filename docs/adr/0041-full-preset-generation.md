# ADR-0041: Full preset generation

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0014, ADR-0021, ADR-0028

## Context

`aps new --preset=full` must create an app like `examples/full-single`: PostgreSQL, runtime settings, jobs, email, authentication, audit, release tracking, operations APIs and the example resource. ADR-0014 requires golden reference apps that CI verifies the generator reproduces. The Minimal preset already works that way: `go generate` turns `examples/minimal` into templates by replacing its placeholder module path and name, and a hand-written `go.mod` template adds the library requirements.

Reading the golden app for the Full preset showed what the Minimal approach doesn't cover:

| Finding | Evidence | Consequence if copied as is |
|---|---|---|
| Golden-only text | README: "This app is the golden copy of what `aps new --preset=full` will generate" | Every new app claims to be the golden copy |
| Links into the apistock repository | README links to `../../docs/guides/authentication.md` and `email.md` | Broken links in every new app |
| A second, unreplaced placeholder | Database user, password and name `acme` in `compose.yaml`, `.env.example`, README, AGENTS | Every app's database is called `acme` |
| Ten apistock modules plus pgx, River and Huma in `go.mod` | `examples/full-single/go.mod` | A hand-written `go.mod` template is a second copy of the dependency list that drifts; Minimal's already lacks the indirect requirements its golden app has |
| Tests that need PostgreSQL | `pgtest` skips without `APISTOCK_TEST_DATABASE_URL` | "The generated app passes its tests" is only true with a database |

Constraints: the golden app stays the single source of truth and hand-written; generated apps work without apistock tooling (architecture principle 8); template injection and writes outside the project stay impossible (threats 2 and 3); the first run stays well under the 60-second target.

## Options

| | 1. Generate committed templates from the golden app | 2. Read the golden app at run time | 3. Hand-written Full templates |
|---|---|---|---|
| Shape | `go generate` writes `cli/internal/recipes/full`; `aps` embeds it; CI regenerates and fails on a diff | `aps new` copies `examples/full-single` from a checkout or download | A second template tree edited by hand |
| Source of truth | Golden app | Golden app | Two copies |
| Works offline, from a released binary | Yes | No: needs a checkout or a network fetch (threat 4: no downloads at install) | Yes |
| Drift detection | Byte-for-byte golden test and the CI diff | None needed | Manual |
| Binary size | About 0.5 MB of templates | None | About 0.5 MB |
| Cost to reverse | Low | Medium | High |
| Fits existing path | Yes: the Minimal pipeline | No | No |

Option 1.

## Decision

### Templates

- `generate.Run` handles both presets: `go generate ./internal/recipes/` writes `minimal/` from `examples/minimal` and `full/` from `examples/full-single`. Templates are never edited by hand.
- Placeholders stay the two the Minimal preset uses: `example.com/acme-api` becomes the module path, then `acme-api` becomes the app name.
- **`go.mod` comes from the golden `go.mod`**, for both presets: every `require` is kept, `apistock.dev` modules take the CLI's library version, the golden `replace` block is dropped, and with `--local` a `replace` is written for every `apistock.dev` module the golden app requires. `go.sum` isn't copied; `aps new` runs `go mod tidy`.
- **Leak check:** generation fails when a golden file (other than `go.mod`) contains a path into the repository (`../../`) or the template delimiters. Golden apps state repository-only facts in the repository's docs, not in their own files.

### Golden app changes

- The development database is named after the app: user, password and database `acme-api`, so a new app's database is its own name. The name pattern (lowercase letters, digits, single hyphens, at most 63 characters) is a valid PostgreSQL role and database name and Compose project name.
- The README no longer calls itself the golden copy, and names the apistock guides instead of linking into the repository.

### `aps new`

| Topic | Decision |
|---|---|
| Presets | `--preset minimal` (default) or `full`; `custom` stays unavailable. In a terminal, a select replaces the "Full arrives in v0.2" note |
| Recipe in `apistock.lock` | `base-full` for Full, `base-minimal` for Minimal, with every created file's hash |
| Content | Exactly the golden app for the name and module: example ping endpoint, heartbeat job and projects resource included, so the output is the reviewed, tested app. The README says how to remove the examples |
| Tenancy and email | Single-tenant; Resend by default. `aps add mail` switches to SMTP afterwards. Tenancy and mail prompts arrive with organisations (v0.4) and the Custom preset |
| Next steps printed | `aps dev` (since 2026-09-15; before that, the commands below), with `cp .env.example .env`, `docker compose up -d --wait`, `go run ./cmd/migrate`, `go run ./cmd/seed` and `go run ./cmd/api` as the path without the CLI; the Mailpit inbox; the seeded administrator; `POSTGRES_PORT` and `DATABASE_URL` when port 5432 is taken |
| `aps dev` with Docker | A separate v0.2 item, done 2026-09-15: see ADR-0028's v0.2 implementation notes |

### Verification

| Level | Check |
|---|---|
| Templates | Golden test per preset: rendering with the placeholders reproduces every golden file byte for byte, and nothing else |
| Generator | Unit tests for the `go.mod` derivation and the leak check |
| CLI | `aps new --preset full` writes the files, lock recipe and module path, and leaves no placeholder or template syntax |
| End to end (`APS_E2E=1`) | `aps new --preset full --local`, then `go vet` and `go test` in the new app (database tests run when `APISTOCK_TEST_DATABASE_URL` is set), then `aps gen resource`, `aps gen job` and `aps gen migration` in it, with a column added to the generated resource's table in that migration, then `go vet` and `go test` again |
| CI | Regenerates both template trees and fails on a diff; the end-to-end job gets a PostgreSQL service so the generated app's database tests run |

## Why

- One source of truth: the app developers read, run and review in this repository is the app they get.
- Deriving `go.mod` removes the one hand-maintained copy of the dependency list, for both presets.
- Fixing leaks in the golden app, plus a check, costs less than new placeholders and keeps templates simple.
- The end-to-end test proves the parts compose: a new Full app accepts the generators users run next.

## Trade-offs

- New apps include example code (ping, heartbeat, projects) to delete or adapt.
- `aps` embeds about 0.5 MB more templates.
- Every golden change regenerates templates in the same pull request; CI enforces it.
- Port 5432 is often taken on developer machines; until `aps dev` checks ports, the printed next steps explain the override.
- Existing local volumes of `examples/full-single` were created with the old `acme` role; `docker compose down -v` resets them.

## Consequences

- `aps new --preset=full` is part of the v0.2 definition of done: register → verify → login → role-protected endpoint → audit event are covered by the generated app's own tests.
- Template changes to either preset follow ADR-0016's upgrade rules from the first release.
- Recipe name `base-full` is recorded in `apistock.lock` and is public (ADR-0015).

## Results (2026-09-15)

| Check | Result |
|---|---|
| Templates | 128 files, about 0.8 MB; both presets reproduce their golden apps byte for byte; the rendered `go.mod` requires and replaces exactly what each golden `go.mod` does |
| New Full app, warm caches, local checkout | `aps new --preset full` including `go mod tidy`: 1 s; `go build ./...`: 3 s; `go test ./...` with `APISTOCK_REQUIRE_DB=1` (every database test runs, including the authentication, projects and releases end-to-end tests): 8 s |
| End to end | New Minimal and Full apps pass `go vet` and their tests; `aps gen resource` and `aps gen job` in the new Full app leave it vetting cleanly |
| Not measured | Cold caches and Docker image pulls, which depend on the network; measured with `aps dev` and Docker |
| Found while implementing | A `--local` path containing a space produced an invalid `go.mod` for both presets; replace paths are now quoted when needed |
