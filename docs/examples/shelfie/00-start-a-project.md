# 0. Start a project

Shelfie is a reading-tracker API: people keep a shelf of books and track what they're reading, from a web app and a mobile app. This chapter creates the project: a `main.go` that runs the whole server, the local database, and the first start. The finished code is in [`examples/apps/shelfie`](https://github.com/gorbital/gorbital/tree/main/examples/apps/shelfie).

> [!NOTE]
> `orb new` creates apps with v0.1's layout until Phase 9 of the [v0.2 roadmap](../../v0.2-roadmap.md). This chapter builds the v0.2 layout by hand; each file is short. Sign-in arrives in chapter 6: until then, routes that need a signed-in reader answer 401, and the tests of [chapter 4](04-tests.md) say who is calling.

## What you'll have

```text
shelfie/
├── cmd/api/main.go                  the program: gorbital.Main
├── db/migrations/                   your migrations, embedded in the binary
├── internal/modules/modules.gen.go  the module list (generated)
├── compose.yaml                     PostgreSQL for development
├── .env.example                     the configuration for development
└── gorbital.yaml                    tells orb dev the app has a database
```

## 1. The module

```bash
mkdir shelfie && cd shelfie
go mod init example.com/shelfie
go get gorbital.dev/gorbital
```

## 2. main.go

`cmd/api/main.go` is the only wiring the app has:

<!-- include examples/apps/shelfie/cmd/api/main.go#main -->

`gorbital.Main` reads the configuration from the environment, connects to PostgreSQL, builds the settings, flags, jobs, email delivery and middleware, and serves the modules' routes until it receives a stop signal ([Your main.go](../../guides/main-go.md)). It also answers commands: `go run ./cmd/api migrate` applies migrations, `go run ./cmd/api openapi` prints the OpenAPI document, `go run ./cmd/api help` lists the rest.

## 3. Migrations

Your tables are goose migrations in `db/migrations`, embedded so the binary carries them:

<!-- include examples/apps/shelfie/db/migrations/migrations.go -->

The library's own tables (runtime settings, jobs, the audit log, rate limits and the rest) aren't in this directory: `migrate` serves them from the library and runs them in one history with yours, ordered by version. The books table arrives in [chapter 1](01-books-module.md).

## 4. The module list

Modules live in `internal/modules/<name>`. `main.go` gets them from `modules.All()`, which `orb` generates:

```bash
mkdir -p internal/modules
orb gen modules
```

```text
✓ Wrote internal/modules/modules.gen.go: no modules
```

Commit the file: the app builds without `orb`. `orb dev` rewrites it whenever you add or remove a module, and `go generate ./internal/modules` does too.

## 5. Configuration and the database

`.env.example` holds what development needs; `orb dev` copies it to `.env`, which is never committed:

<!-- include examples/apps/shelfie/.env.example#env -->

Every variable, with its default and what production refuses, is on the [environment variables](../../guides/environment-variables.md) page. `compose.yaml` runs PostgreSQL on 127.0.0.1:5432:

<!-- include examples/apps/shelfie/compose.yaml -->

And `gorbital.yaml` tells `orb dev` to start it and migrate before starting the app:

<!-- include examples/apps/shelfie/gorbital.yaml -->

## 6. Run it

```bash
orb dev
```

`orb dev` starts PostgreSQL, runs `go run ./cmd/api migrate`, builds and starts the app, and rebuilds it when a file changes. Without the CLI:

```bash
cp .env.example .env
docker compose up -d --wait
set -a; . ./.env; set +a
go run ./cmd/api migrate
go run ./cmd/api
```

Check it's up:

```bash
curl http://127.0.0.1:8080/readyz
```

```json
{"status":"ok","checks":{"postgres":{"status":"ok","duration_ms":1}}}
```

`http://127.0.0.1:8080/docs` shows the API reference, with `GET /version` so far. Stop the app with Ctrl-C: it stops taking requests, lets running work finish and closes the database pool.

## Next

[1. A books module](01-books-module.md): a table, the four layers of a module, and the first routes.
