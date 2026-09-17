# Receiving payment webhooks

A billing API that hears about money from somebody else's system. The payment provider posts an event when a customer pays or is refunded, and the app has to get three things right before it can believe a single row of its own ledger:

- the delivery really came from the provider, and hasn't been changed or captured and sent again;
- the same event, delivered twice, moves the money once;
- whatever the payment sets off — a receipt, a ledger entry, a message to the warehouse — happens if and only if the row was written.

The app is in `examples/apps/payments/`. It has one module, `payments`, laid out like Shelfie's books module ([1. A books module](../shelfie/01-books-module.md)): `domain/`, `usecase/`, `repository/` and `delivery/`, one file per operation.

| Route | Who | Does |
|---|---|---|
| `POST /v1/webhooks/payments` | The provider, proved by the signature | Records `payment.succeeded` and `payment.refunded`; ignores every other type |
| `GET /v1/payments/{id}` | Staff (`payments.payment.read`) | Reads a recorded payment |
| `/ops/*` | Operators (`platform_admin`, `ops_viewer`) | Settings, retention, the receipt job's configuration and runs, audit log |

## main.go

<!-- include examples/apps/payments/cmd/api/main.go#main -->

There is no sign-in module, to keep the recipe to its subject: the provider has no account, and `payments.payment.read` is a permission the app declares but nothing grants, so `GET /v1/payments/{id}` answers 401 until the app adds `gorbital.WithAuth(authhttp.New())` ([Your main.go](../../guides/main-go.md)). The tests say who is calling instead. `opshttp.Module()` is here for one reason — the receipt job below is operated in `/ops/jobs`, where its timeout and retries are changed and a failed receipt is retried ([Ops API reference](../../guides/ops-api.md#job-definitions)).

## The module and the table

One table. A row is one **event** about a payment, not the payment's current state: a payment and its refund are two rows sharing the provider's payment ID. That is what the provider actually tells you, it needs no read-modify-write, and it gives the idempotency below something to be unique about.

<!-- include examples/apps/payments/db/migrations/20260923000001_payments.sql#table -->

| Column | Whose |
|---|---|
| `id` | Ours, `pay_…`. Never the provider's: their IDs are their namespace, and two providers may collide |
| `event_id` | The provider's delivery ID. **Unique**, which is the whole of the idempotency |
| `provider_payment_id` | The provider's payment, shared by that payment's events |
| `amount_minor`, `currency` | Minor units and an ISO 4217 code. Never a float |
| `status` | `succeeded` or `refunded`: which way the money moved |

The module maps its domain errors to problem codes, which are public API:

<!-- include examples/apps/payments/internal/modules/payments/module.go#errors -->

## Verifying the signature

The provider signs each delivery with a secret the two of you share. `guard.Webhook` checks it **before the route's input is parsed**, so a forged request never costs a JSON decode and never learns what validation would have said:

<!-- include examples/apps/payments/internal/modules/payments/delivery/routes.go#routes -->

`guard.Public()` is not a contradiction. The provider has no session, so the authentication the rest of the app relies on does not apply; the signature is what authenticates the request instead. The two guards belong together, and `guard.Webhook` without `guard.Public()` would ask a payment provider to sign in.

This app's provider uses [Standard Webhooks](https://www.standardwebhooks.com), the scheme behind Svix, Resend and Clerk: `webhook-id`, `webhook-timestamp` and `webhook-signature`, HMAC-SHA256 over `"<id>.<timestamp>.<body>"`. `webhook.NewStandard` builds the verifier from the secret:

<!-- include examples/apps/payments/internal/modules/payments/verifier.go#secret-var -->

`Secrets` is a list because rotation is a deploy, not a moment: list the new secret next to the old one, deploy, change the secret in the provider's dashboard, then deploy again with only the new one. A signature made with either is accepted in between.

### What the guard checks, and what it doesn't

| Checked by `guard.Webhook` | Not checked by `guard.Webhook` |
|---|---|
| The body is at most the limit (`guard.WebhookBodyLimit`, 64 KiB here), refused with 413 **before** verifying | That this is the first time this event has arrived |
| The three headers are present and well formed | That the body means anything: the amount, the currency, the event type |
| The signature matches the raw body with one of the secrets, compared in constant time | That the event is about something this app knows |
| `webhook-timestamp` is within five minutes of now, either way | That the provider's own view of the payment still agrees |

Every refusal is the same answer — `401 invalid_webhook_signature` — whether the signature was forged, the body changed, the headers missing or the timestamp stale. A sender learns nothing about which check failed. The handler then reads exactly the bytes that were verified, not a re-read of the request body.

A verifier error that doesn't wrap `webhook.ErrInvalidSignature` — a key server that is down, say — is a 500 rather than a 401, so a broken dependency never looks like a forged request.

## Idempotency

Verification proves *who* sent a delivery. It says nothing about whether you have seen it before, and you will:

- providers retry until they get a 2xx, and a retry carries the same event ID with a **fresh timestamp and signature**, so it verifies perfectly;
- a captured delivery can be replayed for as long as the tolerance allows, five minutes by default;
- your own load balancer, proxy or client library can send one request twice.

So the app decides, not the signature. The delivery ID is the natural key: the provider guarantees it is the same across retries of one event and different between events. The unique index on `event_id` turns "have I seen this?" into something the database answers, under concurrency, without a read the writer takes on trust:

<!-- include examples/apps/payments/internal/modules/payments/repository/insert_payment.go#insert-payment -->

One statement does both halves. The `INSERT … ON CONFLICT DO NOTHING` either inserts or doesn't; the `UNION ALL` branch returns the row that was already there, so a replay is answered with the payment recorded the first time rather than a bare 200 that tells the caller nothing.

`SELECT … WHERE event_id = $1` followed by an `INSERT` would look equivalent and isn't: two deliveries arriving together both read nothing, both insert, and one gets a constraint violation the handler has to unpick — or, worse, the table has no unique index and both rows land. Let the index decide.

The use case reads that `applied` flag and does the rest only when it is true:

<!-- include examples/apps/payments/internal/modules/payments/usecase/record_event.go#record-event -->

| Delivery | Answer |
|---|---|
| New, a type the app records | `200 {"applied":true,"payment_id":"pay_…"}`; the row and its receipt are written |
| The same event again | `200 {"applied":false,"payment_id":"pay_…"}`; nothing changes, the first payment's ID comes back |
| A type the app doesn't record | `200 {"applied":false}`, logged. The provider stops retrying, and adding a handler later is a deploy, not a backfill |
| A negative amount, no payment ID, a currency that isn't a code | `422`, with the module's code. The provider signed it; that doesn't make it true |
| The same event, concurrently, from two instances | `409 delivery_in_progress`. This statement's snapshot cannot see the uncommitted row, so there is nothing truthful to return; the provider's next retry finds it committed |

### Why not `Idempotency-Key`?

gorbital already has [idempotency keys](../../guides/idempotency.md): a client sends `Idempotency-Key`, the middleware stores the first response and replays it for retries. That is the right tool for **your** API's clients, and this app would use it unchanged if it had a `POST /v1/refunds` of its own. It is the wrong tool here.

| | `Idempotency-Key` middleware | The unique `event_id` |
|---|---|---|
| Who chooses the key | Your client, per operation it starts | The provider, per event |
| Scope | The caller: keys belong to a signed-in user or service account, so two callers never share one. A webhook has no actor, so the middleware ignores its header | The table: one event, one row, whoever delivers it |
| What it protects | The whole response, replayed byte for byte | The write |
| How long | `idempotency.retention`, 24 hours by default | For as long as the row exists |

The retention is the deciding difference. A provider can retry a delivery for days, and a payment recorded twice a week later is a real duplicate in a real ledger. The row you already wrote is the record that never expires.

## The side effect, in the same transaction

A recorded payment usually has to set something else off. Doing it inside the request is wrong twice over: the provider is waiting, and the work is retried by nobody if it fails. Doing it after the commit is wrong differently: the process can die between the two.

So the job is enqueued **in the transaction that writes the row**. River stores its queue in the same PostgreSQL database, so `jobs.Client.InsertTx` writes the job through the same `pgx.Tx`:

<!-- include examples/apps/payments/internal/modules/payments/repository/tx.go#tx-manager -->

Use cases never import pgx ([ADR-0022](../../adr/0022-generated-application-layout.md)), so they reach this through a port: a `Tx` is the store of one transaction plus that transaction's jobs.

<!-- include examples/apps/payments/internal/modules/payments/usecase/ports.go#ports -->

The job itself names the payment rather than repeating it, so the worker reads the row that was committed and no amount or address sits in the queue:

<!-- include examples/apps/payments/internal/modules/payments/usecase/receipt_job.go#receipt-args -->

<!-- include examples/apps/payments/internal/modules/payments/usecase/receipt_job.go#receipt-work -->

The module defines it with its code defaults, which operators change at runtime in `/ops/jobs/definitions/payment_receipt`. There is no schedule: nothing runs it on a clock, a recorded payment enqueues it.

<!-- include examples/apps/payments/internal/modules/payments/module.go#jobs -->

### Why this beats an outbox

The transactional outbox pattern exists because the queue is usually somewhere else: you write a row to an `outbox` table in your transaction, and a relay process reads that table and publishes to Kafka, SQS or Redis. The relay, its ordering, its retries and its own idempotency are all yours to build and operate.

Here the queue **is** the database. `InsertTx` writes the job row next to the payment row, in one transaction, and the guarantee follows from PostgreSQL rather than from a relay:

| | Outbox | `jobs.Client.InsertTx` |
|---|---|---|
| Moving parts | Your table, your relay, the broker | River's tables, already migrated |
| Atomicity | The outbox row commits with the write; publishing it does not | The job row commits with the write, and that is the queue |
| Duplicates | At-least-once from the relay, so consumers need their own idempotency | The job is enqueued once because the transaction commits once |
| Ordering and backoff | Yours | River's, with the definition's retries and timeout |
| Operating it | Your dashboards | `/ops/jobs`: runs, failures, retry, cancel |

An outbox earns its keep when the work must leave the database — another team's service, another region, a broker other systems already consume. Until then it is a second copy of a queue you already have. The v0.2 roadmap lists an event bus under *Not in v0.2* for exactly this reason: the pattern on this page covers what most apps mean by it.

## Configuration

<!-- include examples/apps/payments/.env.example#webhook-secret -->

| Situation | What happens |
|---|---|
| The variable is set to a valid secret | Deliveries signed with it are recorded |
| Two secrets, comma-separated | Either is accepted, for the length of a rotation |
| The variable is missing | The app starts, logs a warning, and refuses **every** delivery with 401. The provider retries, so deliveries arrive once the secret is set |
| The variable is malformed | The same: the error names the variable and never quotes the secret |
| `…_FILE` as well as the variable | The app stops at startup: `config: both variable and _FILE variant are set` |

Refusing rather than accepting is the point. A misconfigured deployment that trusted unsigned deliveries would write payments nobody signed, and no later fix removes those rows with any confidence.

## Tests

Run them with PostgreSQL up (`orb dev`, or `docker compose up -d --wait`):

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://payments:payments@127.0.0.1:5432/payments?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test ./...
```

The tests sign deliveries themselves, which is nine lines and worth far more than a mock verifier: the app under test is the one that ships, secret and all.

<!-- include examples/apps/payments/internal/modules/payments/payments_test.go#test-signing -->

A signed delivery is recorded, its receipt is queued, and staff can read it back:

<!-- include examples/apps/payments/internal/modules/payments/payments_test.go#test-records -->

The replay test is the one that matters. It sends the provider's own retry — same event ID, fresh timestamp, fresh signature — and asserts that the second, third and fourth would each change nothing:

<!-- include examples/apps/payments/internal/modules/payments/payments_test.go#test-replay -->

Five unauthentic deliveries, one answer, nothing written:

<!-- include examples/apps/payments/internal/modules/payments/payments_test.go#test-signature -->

A signature is not a warrant for the body's contents:

<!-- include examples/apps/payments/internal/modules/payments/payments_test.go#test-domain -->

And the transactional claim, checked rather than asserted in a comment:

<!-- include examples/apps/payments/internal/modules/payments/payments_test.go#test-rollback -->

| Test | Checks |
|---|---|
| `TestWithoutSecretNothingIsTrusted` | A deployment with no `PAYMENTS_WEBHOOK_SECRET` refuses every delivery instead of trusting it |
| `TestReceiptJobIsListed` (`cmd/api/operations_test.go`) | `payment_receipt` is in `GET /ops/jobs/definitions`, enabled, with no schedule and the definition's retries |
| `TestReadNeedsPermission` (`cmd/api/operations_test.go`) | Reading a payment is staff's, not the world's |
| `TestOpenAPIIsCurrent` (`cmd/api/main_test.go`) | `api/openapi.json` matches the code; after changing a route, run `go run ./cmd/api openapi --dir api` |
| `internal/modules/payments/domain/payment_test.go` | The rules of `NewPayment` and the event types the app records |
| `internal/modules/architecture_test.go` | The layers import only what they may |

Workers don't run under `gorbitaltest`, so a queued job stays queued: [`App.Jobs`](../../methods/gorbital-gorbitaltest.md#App.Jobs) reads it back, which is exactly what these tests want to count.

## What to change for your provider

| If your provider | Change |
|---|---|
| Uses Svix's header names (Resend, Clerk) | `webhook.StandardConfig{Secrets: …, HeaderPrefix: "svix-"}` |
| Signs the body with a hex HMAC and one header (GitHub, Shopify) | `webhook.NewHMAC` with that header, prefix and `webhook.Hex`. **These sign no timestamp, so there is no replay window**: the `event_id` uniqueness is then the only thing standing between you and a replayed delivery |
| Has a scheme of its own (Stripe's `t=…,v1=…`, public-key signatures) | Implement `webhook.Verifier`; [security layers](../../guides/security-layers.md#your-own-verifier) has a worked Stripe one. Return an error wrapping `webhook.ErrInvalidSignature` for anything inauthentic, and any other error for a failure of yours |
| Calls the delivery ID something else | The header name is the verifier's `IDHeader`; the handler reads it with the same `header:"…"` tag. Whatever it is called, it must be **signed**, or the body could claim another event's identity |
| Sends events you don't handle | Nothing: they are logged and answered 200 already. Add a case to `domain.StatusFor` when you want one |
| Sends bodies larger than 64 KiB | `guard.WebhookBodyLimit`. Keep it as small as the provider allows: it is refused before any work is done |
| Retries for days | Nothing: the unique index has no expiry. This is why the `Idempotency-Key` middleware, with its 24-hour retention, is not the tool here |
| Doesn't sign a delivery ID at all | Fall back to a natural key of the event's contents — the provider's payment ID and event type, unique together — and accept that a genuine second refund of the same payment needs a key that distinguishes it |

Two habits that outlast any provider: answer 2xx as soon as the row is committed and do the rest in a job, and make every event type you ignore a deliberate, logged ignore rather than an error the provider will retry all week.

## Related

- [Security layers](../../guides/security-layers.md): the request timeout, the IP filter, signed webhooks and identity-provider tokens, with the table of senders.
- [Idempotency keys](../../guides/idempotency.md): the `Idempotency-Key` middleware for your own API's clients.
- [Background jobs](../../guides/background-jobs.md): definitions, queues, retries and what `/ops/jobs` shows.
- [Methods: `webhook`](../../methods/webhook.md): `NewStandard`, `NewHMAC`, `HMACConfig` and `Verifier`.
- [Methods: `modules/jobs`](../../methods/modules-jobs.md): `Client.Insert`, `Client.InsertTx`, `Definition` and `Manager`.
