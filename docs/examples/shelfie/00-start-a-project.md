# 0. Start a project

Shelfie is a reading-tracker API: people keep a shelf of books and track what they're reading, from a web app and a mobile app. This chapter creates the project with `orb new`, reads the files it writes, and starts the app for the first time. The finished code is in [`examples/shelfie`](https://github.com/gorbital/gorbital/tree/main/examples/shelfie).

Install [Go, Docker and git](../../start/prerequisites.md) first, then the CLI: `go install gorbital.dev/cli/cmd/orb@latest`. If you want the whole tour of a new app — signing up, reading the emails it sends, signing in as the administrator — the [Quickstart](../../start/quickstart.md) is that walk-through; this chapter reads the files instead, because the rest of the tutorial changes them.

## What you'll have

```text
shelfie/
├── cmd/api/                         the program: main.go on gorbital.Main, and
│                                    mail.go and storage.go, one function each
├── db/migrations/                   your migrations, embedded in the binary
├── internal/modules/                your modules, and modules.gen.go, the list main.go reads
├── api/                             the exported OpenAPI document, Postman collection, llms.txt
│                                    and surface.json, the public names the tests freeze
├── compose.yaml                     PostgreSQL for development
├── .env.example                     the configuration for development
├── gorbital.yaml                    how the app was created; the orb CLI reads it
├── go.mod, go.sum                   the Go module and its dependencies
├── Dockerfile, .dockerignore        the production image
├── .gitignore                       .env and .orb/ are never committed
└── README.md, ARCHITECTURE.md, AGENTS.md, AUTH_PROVIDERS.md
```

## 1. Create it

```bash
orb new shelfie --preset full --tenancy single --module example.com/shelfie
```

| Part | Means |
|---|---|
| `--preset full` | PostgreSQL, sign-in, jobs, email, the audit log and the `/ops` APIs. `minimal` is an HTTP API with none of them |
| `--tenancy single` | A book belongs to the reader who added it. `multi` makes data belong to organisations instead; Shelfie stays single-tenant and adds book clubs on top in [chapter 8](08-book-clubs.md) |
| `--module example.com/shelfie` | The Go module path, which every import in the app starts with. Without it the app's name is used |

Leave the flags out to be asked each question instead. `orb new` writes the files, runs `go mod tidy`, and creates a git repository:

```text
✓ wrote 63 files
✓ ran go mod tidy
✓ initialised git
```

It doesn't commit them. Do that yourself: `orb gen` and `orb add` refuse to change an app with uncommitted changes, so every generated change stays a diff you can read.

```bash
cd shelfie
git add -A && git commit -m "Create shelfie"
```

## 2. main.go

`cmd/api/main.go` is the whole wiring of the app — the file below is what `orb new --preset full --tenancy single` writes, with `acme-api` where yours says `shelfie` and `example.com/acme-api` where yours says `example.com/shelfie`:

<!-- include examples/full-single/cmd/api/main.go -->

`gorbital.Main` reads the configuration from the environment, connects to PostgreSQL, builds the settings, flags, jobs, email delivery and middleware, and serves the modules' routes until it receives a stop signal ([Your main.go](../../guides/main-go.md)). `options()` is a function rather than a literal because the tests build the same app from it ([chapter 4](04-tests.md)).

`gorbital.WithAuth(auth)` adds sign-in from the library: registration with email verification, sessions, two-factor authentication, passkeys, Google, Apple and GitHub, and API keys, under `/v1/auth/` ([authentication](../../guides/authentication.md#in-an-app-on-gorbitalmain)). `authhttp.New()` takes its defaults here; [chapter 6](06-accounts.md) gives it options and hooks in `cmd/api/signin.go`. The three modules above it are the library's too: `/ops/` for operators, `GET /v1/flags` for clients, and the webhook that tells the app about bounced email.

`gorbital.Main` also answers commands. `go run ./cmd/api help` lists them:

```text
Usage: shelfie [command]

Commands:
  serve                          run the API server (the default)
  migrate [--status [--json]]    apply pending migrations, or report them
  migrate-down                   roll back the most recent migration (development only)
  openapi [--dir <directory>]    print the OpenAPI document, or write it with the Postman collection and llms.txt
  version [--json]               print the build's version
  roles                          list the platform roles and their permissions
  grant-role <email> <role>      give an account a platform role
  revoke-role <email> <role>     take a platform role away
  reset-mfa <email>              turn off an account's two-factor authentication
  rotate-auth-keys               re-encrypt 2FA secrets with the first AUTH_ENCRYPTION_KEYS key
  auth-providers                 show which sign-in methods are configured
  seed [--email <email>]         create a development administrator with 2FA (orb dev runs it)
```

The last seven, from `roles` down, come from sign-in: a module adds its own commands when `main.go` adds the module. `help` needs no configuration, so it answers before there is a database.

## 3. Migrations

Your tables are goose migrations in `db/migrations`, embedded so the binary carries them:

<!-- include examples/full-single/db/migrations/migrations.go -->

The library's own tables (runtime settings, jobs, the audit log, rate limits, sign-in's accounts and sessions, and the rest) aren't in this directory: `migrate` serves them from the library and runs them in one history with yours, ordered by version. The one migration `orb new` wrote, `db/migrations/20260915000002_projects.sql`, belongs to the sample `projects` module; Shelfie's books table arrives in [chapter 1](01-books-module.md).

Keep at least one `.sql` file here while the app is one you can build: `//go:embed *.sql` fails the build with `pattern *.sql: no matching files found` when the directory is empty.

## 4. The module list

Modules live in `internal/modules/<name>`. `main.go` gets them from `modules.All()`, which `orb` generates:

<!-- include examples/full-single/internal/modules/modules.gen.go -->

`orb gen modules` rewrites it, `orb dev` rewrites it whenever you add or remove a module, and so does `go generate ./internal/modules`. The file is committed, so the app builds without `orb`.

`ping` and `projects` are the two sample modules `orb new` wrote. `ping` is a module without a table: a public endpoint whose reply is a runtime setting, in the layers every module uses. `projects` is what `orb gen module` writes — a table, five routes and the four layers [chapter 1](01-books-module.md) reads one by one. Both are there to be read and then deleted; chapter 1 deletes them, once Shelfie has a migration of its own.

## 5. Configuration and the database

`.env.example` holds what development needs; `orb dev` copies it to `.env`, which `.gitignore` keeps out of the repository. It is long, and commented line by line; these are the ones that matter on the first run:

| Variable | What it does |
|---|---|
| `APP_ENV` | `development` or `production`; required, so a deployment that forgets it doesn't run with development's relaxed checks |
| `APP_ADDR` | Where the API listens; `127.0.0.1:8080` |
| `DATABASE_URL` | PostgreSQL, from `compose.yaml` in development |
| `AUTH_ENCRYPTION_KEYS` | Encrypts two-factor secrets. Empty in `.env.example`; `orb dev` writes a random development key into `.env` when it finds it empty, and production refuses to start without one |
| `MAIL_DELIVERY` | Empty means `devmail` in development: every email lands in the Dev Portal instead of a real inbox |

Every variable, with its default and what production refuses, is on the [environment variables](../../guides/environment-variables.md) page. `compose.yaml` runs PostgreSQL on 127.0.0.1:5432:

<!-- include examples/full-single/compose.yaml -->

And `gorbital.yaml` records how the app was created, which is how `orb` knows what it may generate and what `orb dev` must start:

<!-- include examples/full-single/gorbital.yaml -->

## 6. Run it

```bash
orb dev
```

The first run, in order: copies `.env.example` to `.env`; writes a random `AUTH_ENCRYPTION_KEYS` into it; starts the services in `compose.yaml` with `docker compose up -d --wait`; applies migrations with `go run ./cmd/api migrate`; creates the development administrator with `go run ./cmd/api seed`; builds and starts the app; and opens the Dev Portal at http://127.0.0.1:3100 in your browser, where the app's routes, database, jobs, logs and every email it sends are in one place ([the Dev Portal](../../guides/dev-portal.md)). Its link, and the dev console's token, are new on every run. After that it rebuilds and restarts the app whenever you save a Go file, and applies a new migration before the restart.

> [!WARNING]
> `seed` prints the administrator's password, its two-factor key, an `otpauth://` URI and ten recovery codes, once. They're stored nowhere. Save them, or start again later with `docker compose down -v`. The [Quickstart](../../start/quickstart.md#6-sign-in-as-the-administrator) signs in as this account step by step.

Without the CLI, the same thing by hand:

```bash
cp .env.example .env
sed -i.bak "s|^AUTH_ENCRYPTION_KEYS=.*|AUTH_ENCRYPTION_KEYS=k1:$(openssl rand -base64 32)|" .env && rm .env.bak
docker compose up -d --wait
set -a; . ./.env; set +a
go run ./cmd/api migrate
go run ./cmd/api seed
go run ./cmd/api
```

The app reads the environment, not `.env`: run `set -a; . ./.env; set +a` again in every new terminal, and after editing the file. Without it the app stops with `shelfie: invalid configuration: DATABASE_URL is required`.

Check it's up:

```bash
curl http://127.0.0.1:8080/readyz
```

```json
{"status":"ok","checks":{"postgres":{"status":"ok","duration_ms":1}}}
```

`http://127.0.0.1:8080/docs` is the API reference. A new Full app serves 135 operations before you write a line: `GET /version`, the sample modules' `/v1/ping`, `/v1/echo` and `/v1/projects`, `GET /v1/flags`, the webhook for bounced email, 42 under `/v1/auth/`, and 83 under `/ops/` — settings, feature flags, jobs and queues, the audit log, email, accounts and service accounts, file storage, releases, incidents and observability. Stop the app with Ctrl-C: it stops taking requests, lets running work finish and closes the database pool.

## Next

[1. A books module](01-books-module.md): a table, the four layers of a module, and the first routes.
