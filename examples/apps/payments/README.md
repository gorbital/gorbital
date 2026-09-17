# Payments

A billing API that receives a payment provider's webhooks: each delivery is
verified, recorded once however often it arrives, and its receipt is queued
in the same transaction as the row. It is the app of the recipe *Receiving
payment webhooks* in gorbital's Examples tab
(`docs/examples/recipes/receiving-payment-webhooks.md`); every code block on
that page is included from this directory.

```text
cmd/api/main.go                          gorbital.Main with the built-in ops module, the app's modules and migrations
db/migrations/                           the payments table and its unique index on the provider's event ID
internal/modules/modules.gen.go          the module list (orb gen modules; don't edit)
internal/modules/payments/               a module: module.go, verifier.go, domain/, usecase/, repository/, delivery/
api/                                     the OpenAPI document, a Postman collection and llms.txt
```

| Route | Who | Does |
|---|---|---|
| `POST /v1/webhooks/payments` | The provider, proved by the signature | Records `payment.succeeded` and `payment.refunded`; ignores every other type |
| `GET /v1/payments/{id}` | Staff (`payments.payment.read`) | Reads a recorded payment |
| `/ops/*` | Operators (`platform_admin`, `ops_viewer`) | Settings, retention, jobs, audit log |

What the payments module declares:

| Declaration | Name |
|---|---|
| Permission, held by `platform_admin` | `payments.payment.read` |
| Job | `payment_receipt`, no schedule: a recorded payment enqueues it |
| Environment variable | `PAYMENTS_WEBHOOK_SECRET` (`whsec_…`, or two while it is rotated) |

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

Without `PAYMENTS_WEBHOOK_SECRET` the app starts and logs a warning, and
every delivery is refused with 401 `invalid_webhook_signature`. Point the
provider's test webhook at `/v1/webhooks/payments`, or sign a delivery
yourself the way `internal/modules/payments/payments_test.go` does.

Sign-in, which grants staff the `platform_admin` role, arrives in a later
phase: until then `GET /v1/payments/{id}` answers 401 and `orb dev`'s dev
console token operates `/ops/` locally.

## Test it

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://payments:payments@127.0.0.1:5432/payments?sslmode=disable' go test ./...
```

After changing a route: `go run ./cmd/api openapi --dir api`. After adding
or removing a module: `go generate ./internal/modules` (or let `orb dev` do
it).
