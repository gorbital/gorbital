# Mobile backend

The API behind a travel app whose users sign in with an external identity
provider (Auth0, Clerk, Supabase, Firebase, Cognito). The app stores no
passwords and issues no sessions: it verifies the provider's access tokens
and keeps each traveller's trips. It is the app of the recipe *Mobile
backend with an external identity provider* in gorbital's Examples tab
(`docs/examples/recipes/mobile-backend-with-an-idp.md`); every code block on
that page is included from this directory.

```text
cmd/api/main.go                     gorbital.Main with the identity provider as the app's authenticator
cmd/api/idp.go                      IDP_* to jwt.Config, and when the provider's keys are fetched
db/migrations/                      the app's own migrations
internal/modules/modules.gen.go     the module list (orb gen modules; don't edit)
internal/modules/trips/             a module: module.go, domain/, usecase/, repository/, delivery/
api/                                the OpenAPI document, a Postman collection and llms.txt
```

| Route | Needs | Does |
|---|---|---|
| `POST /v1/trips` | `trips.trip.write` | Adds a trip for the caller |
| `GET /v1/trips` | `trips.trip.read` | Lists the caller's trips, newest first |
| `GET /v1/trips/{id}` | `trips.trip.read` | Reads one of the caller's trips |
| `DELETE /v1/trips/{id}` | `trips.trip.write` | Deletes one of the caller's trips |

Both permissions come from the token's permissions claim, and every route is
scoped to the caller: another traveller's trip is `404 trip_not_found`.

## Run it

Point `IDP_ISSUER`, `IDP_AUDIENCE` and `IDP_JWKS_URL` at your provider's
tenant (see `.env.example`), then:

```bash
orb dev
```

Without the CLI:

```bash
cp .env.example .env   # and edit the IDP_* variables
docker compose up -d --wait
set -a; . ./.env; set +a
go run ./cmd/api migrate
go run ./cmd/api
```

The app fetches the provider's keys when it starts, so a mistyped issuer or
JWKS URL stops it there rather than failing requests later. Call it with a
token your provider issued:

```bash
curl -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8080/v1/trips
```

## Test it

The tests need no provider: they publish a key set from an `httptest`
server and sign their own tokens (`internal/modules/trips/idp_test.go`).

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://mobile_backend:mobile_backend@127.0.0.1:5432/mobile_backend?sslmode=disable' go test ./...
```

After changing a route: `go run ./cmd/api openapi --dir api`. After adding
or removing a module: `go generate ./internal/modules` (or let `orb dev` do
it).
