# Shelfie

A reading-tracker API for a web and a mobile app: the example application
of gorbital's Examples tab, built chapter by chapter. Every code block on
those pages is included from this directory.

```text
cmd/api/main.go                 gorbital.Main with the app's modules and migrations
cmd/api/signin.go               sign-in's options and hooks, and the SMS sender
db/migrations/                  the app's own migrations
internal/modules/modules.gen.go the module list (orb gen modules; don't edit)
internal/modules/books/         a module: module.go, domain/, usecase/, repository/, delivery/
internal/modules/profiles/      readers' profiles, filled at registration (chapter 6)
internal/modules/phonelogin/    phone-code sign-in through authhttp's SignIn (chapter 7)
api/                            the OpenAPI document, a Postman collection and llms.txt
```

## Run it

```bash
orb dev
```

Without the CLI:

```bash
cp .env.example .env
docker compose up -d --wait
set -a; . ./.env; set +a
go run ./cmd/api migrate
go run ./cmd/api
```

Readers register with `POST /v1/auth/register`, which also takes a
`display_name` (chapter 6). In development, phone sign-in codes are written
to the log instead of texted (chapter 7).

## Test it

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://shelfie:shelfie@127.0.0.1:5432/shelfie?sslmode=disable' go test ./...
```

After changing a route: `go run ./cmd/api openapi --dir api`. After adding
or removing a module: `go generate ./internal/modules` (or let `orb dev` do
it).
