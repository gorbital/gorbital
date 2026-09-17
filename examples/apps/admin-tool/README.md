# Admin tool

An internal back-office API: staff publish announcements that customers
see, and operators tune the app through `/ops`. It is the app of the
recipe *Internal admin tool* in gorbital's Examples tab
(`docs/examples/recipes/internal-admin-tool.md`); every code block on that
page is included from this directory.

```text
cmd/api/main.go                          gorbital.Main with the built-in ops and flags modules, the app's modules and migrations
db/migrations/                           the app's own migrations
internal/modules/modules.gen.go          the module list (orb gen modules; don't edit)
internal/modules/announcements/          a module: module.go, domain/, usecase/, repository/, delivery/
api/                                     the OpenAPI document, a Postman collection and llms.txt
```

What the announcements module declares:

| Declaration | Name |
|---|---|
| Permission, held by `platform_admin` | `announcements.announcement.write` |
| Runtime setting | `announcements.max_active` (3, 1 to 20) |
| Retention | `expired_announcements`, setting `announcements.retention` (90 days, 1 day to 3 years) |
| Client flag | `announcements.banner` |
| Rate limiter | `announcements_publish` (20 publishes an hour per staff member) |

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

`GET /v1/announcements` is public. Sign-in, which grants staff the
`platform_admin` role, arrives in a later phase: until then publishing
answers 401, `orb dev`'s dev console token operates `/ops/` locally, and the
tests say who is calling with gorbitaltest.

## Test it

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://admin_tool:admin_tool@127.0.0.1:5432/admin_tool?sslmode=disable' go test ./...
```

After changing a route: `go run ./cmd/api openapi --dir api`. After adding
or removing a module: `go generate ./internal/modules` (or let `orb dev` do
it).
