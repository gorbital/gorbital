# Admin tool

An internal back-office API: staff publish announcements that customers
see, and operators tune the app through `/ops`. It is the app of the
recipe *Internal admin tool* in gorbital's Examples tab
(`docs/examples/recipes/internal-admin-tool.md`); every code block on that
page is included from this directory.

```text
cmd/api/main.go                          gorbital.Main with sign-in, the built-in ops and flags modules, the app's modules and migrations
db/migrations/                           the app's migrations, sign-in's included
internal/modules/modules.gen.go          the module list (orb gen modules; don't edit)
internal/modules/announcements/          a module: module.go, domain/, usecase/, repository/, delivery/
internal/modules/auth/                   sign-in: login, password reset, sessions, second factors, API keys, /ops/auth/users
api/                                     the OpenAPI document, a Postman collection and llms.txt
```

Sign-in (`internal/modules/auth`) is the app's own code, not the library's:
`orb eject` copied it from gorbital so that every step can be read here. It
is laid out like the app's other modules: the root package with its options
and hooks, then `domain/`, `usecase/` (`login.go`, `password.go`,
`sessions.go`, `mfa.go`, `apikeys.go`, `operators.go`, …), `repository/`
with one SQL statement per file, and `delivery/` with the routes and jobs,
with the tests that came with it. Its migrations are in `db/migrations`
under the versions the library gives them (`20260915000001_auth.sql`, …), so
a database migrated before the copy applies nothing new, and `gorbital.lock`
records the copy. The package keeps its name, `authhttp`, so no call
changed; the API, the tables and the behaviour are the library's. Library
releases no longer change this code, and `orb doctor` warns when the
library's copy does ([Ejecting a
module](../../../docs/guides/ejecting-a-module.md)).

What the announcements module declares:

| Declaration | Name |
|---|---|
| Permission, held by `platform_admin` and `announcements_editor` | `announcements.announcement.write` |
| Platform role, requiring a second factor | `announcements_editor` |
| Audit actions | `announcements.announcement.published`, `announcements.announcement.withdrawn` |
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

`GET /v1/announcements` is public; everything else needs a signed-in
account. Sign-up is closed (`authhttp.WithoutRegistration`), so the first
account comes from `go run ./cmd/api seed` (which `orb dev` runs) or from
`POST /ops/auth/users`, and gets its role with `go run ./cmd/api grant-role
<email> platform_admin`, or `announcements_editor` for staff who only
publish. `orb dev`'s dev console token operates `/ops/` locally, from
loopback only.

Sign-in's options are in `signInOptions` (`cmd/api/main.go`): no sign-up,
a 16-character password minimum, API keys expiring within 30 days, and a
second factor for `announcements_editor`.

## Test it

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://admin_tool:admin_tool@127.0.0.1:5432/admin_tool?sslmode=disable' go test ./...
```

`cmd/api/operations_test.go` says who is calling with gorbitaltest
principals; `cmd/api/signin_test.go` builds the app with `signInOptions`
and uses accounts an operator creates.

After changing a route: `go run ./cmd/api openapi --dir api`. After adding
or removing a module: `go generate ./internal/modules` (or let `orb dev` do
it).
