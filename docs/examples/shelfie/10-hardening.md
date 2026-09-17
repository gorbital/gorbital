# 10. Hardening and partners

Shelfie has readers, operators and now a business partner: Pagebound, a bookshop that tells Shelfie when one of its customers buys a book, so the reader sees it without typing the title in. A partner is a stranger with a key. This chapter adds the route that receives its webhooks, and tightens what the rest of the API lets any caller do: how long a request may take, who reaches `/ops/`, and how often anyone may call anything ([security layers](../../guides/security-layers.md)).

None of it is authentication. Each layer removes one way a request can hurt the app before the request is even understood.

## 1. Verify the partner's deliveries

Pagebound signs each delivery with a secret the two of you share, in the [Standard Webhooks](https://www.standardwebhooks.com) scheme: a delivery ID, the time it was signed, and an HMAC-SHA256 of all three over the raw body. `guard.Webhook` checks it **before the body is parsed**, so a forged request never costs a JSON decode and never learns what validation would have said.

<!-- include examples/apps/shelfie/internal/modules/partners/delivery/routes.go#routes -->

| Option | Why |
|---|---|
| `guard.Public()` | A shop has no Shelfie session. Deny by default holds everywhere else ([chapter 2](02-protecting-routes.md)) |
| `guard.Webhook(v, guard.WebhookBodyLimit(32<<10))` | The signature, then a body limit well under the 1 MiB default: a purchase is a few hundred bytes, and a larger body is answered 413 before it is read |
| `guard.RateLimit(600, time.Minute, guard.ByIP(), guard.Named("partner_webhooks"))` | Pagebound sends bursts after a sale, never thousands a minute. `ByIP` because there is no signed-in caller to count; the name puts it in `/ops/auth/rate-limits`, where an operator can see and reset it |
| `gorbital.Timeout(5*time.Second)` | The shop's client gives up after ten seconds, so answering later only produces a retry it has already scheduled |

Every refusal is `401 invalid_webhook_signature`, whatever was wrong: a missing header, another secret, a changed body, a timestamp outside the five-minute window. A sender that learns *which* check failed learns how to pass it.

## 2. The secret, and rotating it

The secrets come from the environment in `main.go`, and the module takes them, so nothing below `cmd/api` reads the environment:

<!-- include examples/apps/shelfie/cmd/api/partners.go#partner-secrets -->

<!-- include examples/apps/shelfie/internal/modules/partners/module.go#module -->

Two behaviours are worth naming:

- **No secret refuses everything.** An app that isn't configured for Pagebound must not accept what Pagebound sends; the route stays in the OpenAPI document either way, so clients generated from it don't change with a deployment's configuration.
- **A secret the library won't take stops the app**, through `Platform`, with the other configuration errors and before it listens — not on the first delivery at three in the morning.

Rotating is the reason `Secrets` is a list: add the new secret, deploy, let Pagebound switch, remove the old one.

## 3. A signature is not a fact

Verification proves *who* sent a request. It says nothing about whether the app has seen it before, or whether what it says makes sense.

**Seen before.** Senders retry, and a captured delivery can be replayed inside the signature's tolerance. Pagebound's own ID for the delivery is unique per partner in the table, and one statement decides:

<!-- include examples/apps/shelfie/db/migrations/20260920000006_partner_purchases.sql#partner-purchases -->

<!-- include examples/apps/shelfie/internal/modules/partners/repository/insert_purchase.go#insert-purchase -->

so a repeat returns the purchase the first delivery stored, without a second row and without a second audit event:

<!-- include examples/apps/shelfie/internal/modules/partners/usecase/record_purchase.go#record-purchase -->

The recipe [Receiving payment webhooks](../recipes/receiving-payment-webhooks.md) takes the same pattern further, and enqueues the side effect in the transaction that writes the row.

**Makes sense.** The domain checks every field, exactly as it would for a reader's own request:

<!-- include examples/apps/shelfie/internal/modules/partners/domain/purchase.go#new-purchase -->

## 4. Test what the partner can't do

The tests sign deliveries the way Pagebound does, so the bytes that are signed are the bytes the app verifies:

<!-- include examples/apps/shelfie/internal/modules/partners/partners_test.go#sign -->

A delivery is recorded once:

<!-- include examples/apps/shelfie/internal/modules/partners/partners_test.go#replay-test -->

and nothing unauthentic is recorded at all:

<!-- include examples/apps/shelfie/internal/modules/partners/partners_test.go#refused-test -->

`TestASignedDeliveryStillHasToBeValid` sends correctly signed nonsense and expects the module's own codes; `TestPurchasesAreTheReadersOwn` checks that `GET /v1/purchases` never leaks another reader's; `TestWithoutASecretEveryDeliveryIsRefused` builds the app with no secret at all.

## 5. Timeouts everywhere else

Apps on `gorbital.Main` already have a request timeout: `APP_REQUEST_TIMEOUT`, `30s` by default, as the `Timeout` step of the [middleware stack](../../guides/middleware-stack.md). When it passes, the client gets `503 request_timeout` with the request ID, and the handler's context is cancelled, so the queries Shelfie passes `ctx` to stop too.

`gorbital.Timeout(d)` on a route or a group can only **shorten** it — a context deadline can't be extended. For something genuinely slow, raise `APP_REQUEST_TIMEOUT` or move the work into a [background job](../../guides/background-jobs.md); don't leave one route holding a database slot for two minutes.

```bash
# .env: below the load balancer's idle timeout, above the slowest honest request
APP_REQUEST_TIMEOUT=30s
```

## 6. Keep `/ops` to your network

[Chapter 5](05-operations.md) set `OPS_ALLOWED_IPS`, and it is worth repeating here because it belongs to this list:

<!-- include examples/apps/shelfie/.env.example#ops-allowed-ips -->

Every `/ops/` route runs the IP filter first, before the sign-in check, the guards and the input parsing: an address outside the list gets `403 ip_not_allowed` and learns nothing else. The address compared is the client's **after** `APP_TRUSTED_PROXIES`, so behind a load balancer list the balancer there, or every request appears to come from it.

This is not authentication. `/ops` still needs a session, a role and a second factor; an attacker inside the VPN gets no further than before.

## 7. Per-route rate limits

Shelfie's limits are on the routes that cost something or can be guessed at:

| Route | Limit | Keyed by |
|---|---|---|
| `POST /v1/books` | 30 a minute | The signed-in reader |
| `PUT /v1/phone` | 5 an hour | The signed-in reader |
| `POST /v1/phone-sign-in/code` | 10 an hour | The client address ([chapter 7](07-phone-code-sign-in.md)) |
| `POST /v1/phone-sign-in` | 20 an hour | The client address |
| `POST /v1/webhooks/partners/purchases` | 600 a minute | The client address, as `partner_webhooks` |

Over the limit is `429 rate_limited` with `Retry-After`. With the shared store in `Deps.RateLimits` the budget is one across every instance; without it each instance counts on its own, which is a different limit than you think you set. Sign-in's own limits are separate and already there ([configuring sign-in](../../guides/configuring-sign-in.md)).

`GET /ops/auth/rate-limits` lists every named limiter, Shelfie's and sign-in's, and an operator can reset one when a real reader is locked out.

## 8. What the partner sees

```bash
curl -i https://api.shelfie.example/v1/webhooks/partners/purchases \
  -H 'Content-Type: application/json' \
  -H "webhook-id: evt_8xk2" -H "webhook-timestamp: $(date +%s)" \
  -H "webhook-signature: v1,$SIGNATURE" \
  -d '{"event_id":"evt_8xk2","user_id":"usr_ada","isbn":"9780140449136","title":"The Odyssey","purchased_at":"2026-09-20T10:00:00Z"}'
```

| What went wrong | Answer |
|---|---|
| Nothing | `200` with the purchase, the same on every retry |
| The signature, the timestamp or the body | `401 invalid_webhook_signature` |
| A body over 32 KiB | `413 request_too_large`, before verifying |
| A field the rules refuse | `422` with the module's code (`invalid_isbn`, `invalid_reader`, …) |
| Too many deliveries | `429 rate_limited` with `Retry-After` |
| Shelfie took longer than five seconds | `503 request_timeout` |

## Next

[11. Deploy](11-deploy.md): the production environment, migrations in the pipeline, health checks and the image.
