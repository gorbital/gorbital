# Shelfie

A reading-tracker API for a web and a mobile app: the example application
of gorbital's Examples tab, built chapter by chapter. Every code block on
those pages is included from this directory.

```text
cmd/api/main.go                 gorbital.Main with the app's modules and migrations
db/migrations/                  the app's own migrations
internal/modules/modules.gen.go the module list (orb gen modules; don't edit)
internal/modules/books/         a module: module.go, domain/, usecase/, repository/, delivery/
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

Sign-in arrives in a later chapter: until then, every route of the books
module answers 401, and the tests sign in with gorbitaltest.

## Test it

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://shelfie:shelfie@127.0.0.1:5432/shelfie?sslmode=disable' go test ./...
```

After changing a route: `go run ./cmd/api openapi --dir api`. After adding
or removing a module: `go generate ./internal/modules` (or let `orb dev` do
it).
