# 15. Paying for an order, and a rule across two modules

A restaurant should not start cooking food nobody has paid for. That sentence is the whole chapter, and it turns out to be the most awkward thing Plateful asks the framework to do — not because taking money is hard, but because the rule has one half in the payments module and the other half in the orders module, and gorbital has no way to write it down in one place.

We'll build the payments module first: a payment row per order, a status machine, a provider behind a port, a signed webhook that confirms the money moved, and two independent kinds of idempotency. Then we'll look at the cross-module rule honestly, including the part where renaming a database column silently breaks the app and every test still passes.

## 1. One payment per order, and a status machine

**What we're doing.** Giving a payment its own table and its own states.

**Why.** "Paid" is not a boolean. Between a customer tapping *Pay* and the money being settled there are several distinct situations — asked for and not answered, authorised but not taken, taken, given back, refused — and a restaurant may only start cooking in some of them. Collapsing that into `orders.paid` is how you end up cooking for a payment that was declined thirty seconds later.

**What the framework already gives us.** Nothing at all. There is no payment abstraction in gorbital, no money type, no ledger. This is entirely the app's.

**What we build ourselves.** A `order_payments` table with a `UNIQUE` on `order_id`, and a status machine in the domain layer:

<!-- include examples/apps/plateful/internal/modules/payments/domain/payment.go#payment-status-machine -->

**What just happened.** The rules about how money may move are a table, not a chain of `if`s in a handler. That matters more here than it did for the order's own state machine, because two completely different callers move a payment — the provider's webhook and a platform refund — and a rule written into one handler is a rule the other handler does not have.

Two statuses appear in the `transitions` map as keys with no entry, which is what makes them final: nothing moves out of `refunded` or `failed`. `Final()` is that fact, read off the same table rather than duplicated as a list.

## 2. The call the customer makes

**What we're doing.** `POST /v1/orders/{orderId}/pay`.

**Why.** Somebody has to ask for the money, and it has to be safe to ask twice, because phones lose signal in exactly the second between sending a request and reading its answer.

**What the framework already gives us.** Two things worth naming. First, a permission check on the route. Second — and this is the one people don't expect — **`Idempotency-Key` handling for free**. Every POST and PATCH in a Full app accepts the header: the first request runs, its response is stored, and a retry with the same key gets the same response with `Idempotent-Replayed: true`, without the handler doing anything ([idempotency guide](../guides/idempotency.md)).

**What we build ourselves.** The ownership check, and a second, separate kind of idempotency that doesn't depend on the client at all.

**How.**

<!-- include examples/apps/plateful/internal/modules/payments/usecase/pay_order.go#pay-order -->

**What just happened.** Two things, and they are independent.

**The ownership check is in the use case, not in a guard.** A customer belongs to no organisation, so `guard.OrgMember` has nothing to ask about them; the route's `guard.Permission(PermPay)` only establishes "some signed-in account", which every account is. What makes this order *this caller's* is a column in the orders table, and a guard cannot read a row — `guard.Request` sees the path, the headers and the query and nothing else. So the rule lives where the row is read. An order belonging to somebody else answers `ErrOrderNotFound`, exactly as an order that does not exist does, so order IDs cannot be probed by paying for them.

**The operation is idempotent without the client's help.** An order that already has a pending payment gets *that* payment back rather than a second one, and the status is 201 either way. That is deliberate: `Idempotency-Key` protects against the client retrying the same request; this protects against the client sending a *new* request for something already in flight — a second tab, a second device, a customer who pressed the button again a minute later. The two are layers, not alternatives, and this one is the layer that still works when the client forgets the header.

## 3. The provider is a port

**What we're doing.** Deciding where a real payment provider's SDK will one day go.

**Why.** A payment provider is the most vendor-specific thing an app touches, and also the thing an app is most likely to change. If the provider's vocabulary leaks into the status machine, the routes and the tests, then swapping it is a rewrite.

**What the framework already gives us.** Nothing. gorbital has no payment integration and takes no position on providers.

**What we build ourselves.** An interface with two methods, in the use case layer, and one adapter behind it:

<!-- include examples/apps/plateful/internal/modules/payments/usecase/ports.go#payment-provider-port -->

**What just happened.** No payment provider's API is described anywhere in this module. `Pay` asks for money and gets back a reference; the answer arrives later as a signed webhook. That is the shape of every card provider worth using, and it is the only thing the rest of the module knows about them.

The adapter is `internal/modules/payments/provider.go`, forty lines that generate a fake reference and always agree, and it is the *only* file a real integration replaces. The status machine, the routes, the idempotency and the audit trail are written against `usecase.Provider` and the webhook body, not against a vendor. In tests it is the same adapter — Plateful does not need a mock, because the real one already takes no money.

This is worth stating as a general rule, because it is the payoff of the layering the whole app uses: **a port is a place you can be wrong later**. The two methods above are a guess at what a provider needs. If the guess is wrong, the damage is confined to one file and one interface, and the compiler shows you every caller.

## 4. The provider's event, and the idempotency that matters

**What we're doing.** Receiving `POST /v1/webhooks/payments` and applying what it says.

**Why.** The money moves at the provider, not here. The only way this app learns that a payment was authorised is that somebody tells it, and "somebody tells it" is a route with no session that changes the state of money — which is about as sharp an edge as an API has.

**What the framework already gives us.** `guard.Webhook(verifier)`, which reads the raw body once up to a limit, hands it with the headers to a `webhook.Verifier`, refuses a bad signature with 401 `invalid_webhook_signature` **before the route's input is parsed**, and gives the same verified bytes to the handler. `webhook.NewStandard` implements the [Standard Webhooks](https://www.standardwebhooks.com) scheme, which signs `<webhook-id>.<webhook-timestamp>.<body>` — so the whole body, including the event ID, is covered. A body over the limit is 413 before anything is verified.

**What we build ourselves.** The verifier's construction from a secret, and the rule that an event is applied exactly once:

<!-- include examples/apps/plateful/internal/modules/payments/usecase/apply_event.go#apply-payment-event -->

**What just happened.** Read the three bullets in that comment again, because each is a decision that goes wrong in production if you get it backwards.

**An unknown event type is accepted and ignored.** A provider's vocabulary grows without warning. Refusing a word this app has not learned yet buys an endless retry loop at the provider and nothing else.

**A redelivery is stopped by the event ID, not by the signature.** The provider retries a delivery it is not sure arrived, and signs it afresh each time, so a valid signature says nothing about whether the event has already been applied. What says so is `payment_events.event_id`, the table's primary key, inserted **in the same transaction** as the status change. A redelivery loses the insert and leaves the payment alone. This is the `guard.Webhook` documentation's own advice — "make the handler idempotent, for example by storing the delivery ID with the change it makes" — and it is not optional.

**The payment is locked before it is read.** Two deliveries about one payment queue up rather than both reading the same version and both trying to write it.

One more thing about the verifier, in `payments/verifier.go`: when `PAYMENTS_WEBHOOK_SECRET` is missing or unusable, it returns a verifier that **accepts nothing** rather than one that accepts everything. A deployment that forgot the variable refuses every delivery and the provider retries until somebody notices, which is a bad afternoon. The alternative — marking orders paid on the word of anyone who can reach the URL — is a bad company.

## 5. Three callers, three guards

<!-- include examples/apps/plateful/internal/modules/payments/delivery/routes.go#payment-routes -->

The three route groups are the module in miniature, and no two of them look alike: a platform permission plus a use-case ownership check for the customer, `guard.Public()` plus `guard.Webhook` for a caller with no session at all, and a permission plus `guard.RecentReauth()` for platform staff moving money. Guards are covered in the [guards and middleware guide](../guides/guards-and-middleware.md); what's worth taking from here is that "which guard" is a question with a different answer per route, and copying the neighbouring route is how it gets answered wrongly.

## 6. The rule across two modules, honestly

**What we're doing.** Stopping a restaurant accepting an order whose payment is not authorised.

**Why.** It is the actual business rule. Everything above is machinery for it.

**What the framework already gives us.** Nothing — and this is the chapter's real subject, so it is worth being precise about what "nothing" means. gorbital has modules, and modules have `Errors`, `Permissions`, `Settings`, `Flags`, `Jobs`, `Routes` and `Migrations`. It has no cross-module rule engine, no shared domain layer, no event bus, and no supported way for one module's use case to call another's inside the same transaction.

Plateful's own layering test makes that a hard boundary rather than a habit. `internal/modules/architecture_test.go` walks every file and fails the build when one module imports another module's `domain`, `usecase`, `repository` or `delivery` package. A module may import another module's **root** package and nothing below it.

**What we build ourselves.** The refusal, in the orders module, where accepting happens:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/accept_order.go#accept-order -->

reading the other module's state with SQL of its own, inside the same transaction that locks the order:

<!-- include examples/apps/plateful/internal/modules/orders/repository/store.go#payment-status-sql -->

and an error whose comment says where the other half is:

<!-- include examples/apps/plateful/internal/modules/orders/domain/errors.go#payment-error -->

**What just happened.** The rule now works. A restaurant accepting an unpaid order gets 409 `payment_not_authorised`, and the check happens under the same lock as the state transition, so there is no window between "is it paid?" and "accept it".

And here is the honest part.

**The two halves are joined by a column name and a string literal, and nothing else.**

- The payments module owns `order_payments`. It writes `status`, and its status values are `pending`, `authorised`, `captured`, `refunded`, `failed`.
- The orders module reads `order_payments.status` in hand-written SQL and compares it against two string constants it declares itself: `paymentAuthorised = "authorised"` and `paymentCaptured = "captured"`.

Now consider a perfectly reasonable refactor in the payments module: rename the column `status` to `state`, migration and all. The payments module compiles. Its tests pass — they exercise the payments module's own store, which was renamed with it. The orders module compiles, because its SQL is a string. **The orders module's tests pass too**, because the test helper that authorises a payment writes the `order_payments` row directly, with the module that owns it left out of the test's app entirely:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-authorise-payment -->

Rename the column in the migration and this helper's `INSERT` changes with it, or the test fails loudly — but the *production* path, `SELECT status FROM order_payments`, is a different string in a different module, and nothing connects them. In the worst arrangement of those edits, both modules are green and every restaurant on the platform can accept unpaid orders.

There is no trick that fixes this within the framework's rules. A shared Go constant would be an import across modules. A foreign key can enforce that the row exists but not what a status string means. A view is still a name in two places. The mitigations that actually help are small and unglamorous:

- **Say it in the package comment**, at the top of `payments/module.go`, in the words a person doing the rename will be reading: *"the names `order_payments.order_id` and `order_payments.status` are public API of this module in exactly the way an error code is"*.
- **Say it at both ends** — in the reading SQL, in the constants, in the error's doc comment — so grep finds it from either side.
- **Write an integration test that runs both modules**, not just one. Plateful's orders tests deliberately exclude the payments module to keep them focused, which is what makes the trap possible; an app where this rule is load-bearing should have at least one test where a real webhook authorises a real payment and a real acceptance succeeds.

This is a genuine limitation, not a stylistic preference. Modules keep a codebase from turning into a ball of mud, and the price is that a rule spanning two of them is written twice, in a language the compiler does not check. Knowing where those seams are is part of owning the app.

## Where to go next

- Signed webhooks in general, including rotation and the verifier interface: [security layers guide](../guides/security-layers.md#signed-webhooks).
- What `Idempotency-Key` does and does not replay: [idempotency guide](../guides/idempotency.md).
- Guards, and why each route picks a different one: [guards and middleware](../guides/guards-and-middleware.md).
- Next chapter: [reviews and the one public route](16-reviews-and-the-public-route.md), a third shape of ownership and Plateful's only endpoint that needs nobody.
