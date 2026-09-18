# 12. Your own function

Everything so far has followed a shape the generators know: a resource, some CRUD, a guard on each route. Real applications stop fitting that shape almost immediately. *Give this order to that courier. Refuse orders while the restaurant is closed. Stop all ordering for ten minutes while we migrate. Every five minutes, find the orders a kitchen is sitting on.*

None of those is "create a thing". This chapter shows the four shapes you will reach for, with the real code for each, and — the part that matters — how to tell which one you want.

| You want to… | Reach for | Lives in |
|---|---|---|
| perform an operation with rules of its own | a use case + a thin handler | `usecase` / `delivery` |
| refuse a request based on *who or what* it is, before the handler | a custom guard, `guard.New` | `delivery` |
| affect every route in a group, regardless of the caller | module middleware, `gorbital.Use` | `delivery` |
| do work no client ever asks for | a use case with **no route**, called by a job | `usecase` |

---

## Shape A — an operation that is not CRUD

**What we are doing.** `POST /v1/orgs/{orgId}/orders/{id}/courier` — a restaurant hands an order to a courier.

**Why this shape.** It has rules of its own, it changes more than one row, and those rows are in two different worlds: an order belongs to an organisation, a courier belongs to none. It is not an update to an order with a different name.

**What the framework already gives us.** The route (`gorbital.Post` with `guard.OrgMember(usecase.PermManage)`), request parsing, the transaction manager from [chapter 9](09-one-transaction.md), the audit recorder, and error mapping.

**What we build ourselves.** The operation, in `usecase`, with the two rows locked and changed together.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/usecase/assign_courier.go#assign-courier -->

The handler is the usual four lines — read the request, call the use case, shape the answer — and the route is one entry in the staff group with `guard.OrgMember(usecase.PermManage)`. The domain decides whether the *order* can change hands at all (`Order.AssignCourier` refuses a terminal or already-collected order), and `SetCourierOrder` in the repository claims the courier with a conditional `UPDATE`, so a courier who was taken between the dispatch list and this call makes the assignment fail rather than double-book.

**What just happened.** Three things worth copying whenever you write an operation like this.

**Order the writes so the risky one fails first.** The courier is claimed *before* the order is updated, because the courier is the row somebody else might be competing for. If it fails, nothing has changed.

**Make it idempotent where you sensibly can.** Assigning the same courier again returns the order unchanged rather than erroring. A restaurant tapping twice on a flaky connection should not get a `409`.

**Say plainly what is awkward.** The doc comment does not pretend this is tidy: one transaction changes a tenant's row and a platform-scoped row, the couriers table is read and written with SQL because a module never imports another module's layers, and there is no way to call another module's use case inside this transaction. Writing that down is more useful than a clean-looking abstraction that hides it.

There is a variation of the same operation nearby: when an order becomes ready and nobody is carrying it, the platform may pick a courier itself. That is the same work behind a feature flag:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/assign_courier.go#auto-assign-flag -->

Note `Evaluate` rather than `Enabled`: when nothing is assigned, "the flag is off for everyone" and "this restaurant is outside the rollout" are different problems, and only one of them is a bug ([feature flags](../guides/feature-flags.md)).

---

## Shape B — your own guard

**What we are doing.** Refusing to take an order at a restaurant that is not open, before the handler runs.

**Why this shape.** The question is about the *request*, not about the operation's data: is this restaurant taking orders at all? A guard answers questions like that, gets its statuses into the OpenAPI document, appears in `orb routes`, and keeps unwanted traffic out of the handler.

**What the framework already gives us.** `guard.New`, and a `Request` with a deliberately small surface ([`gorbital/guard`](../methods/gorbital-guard.md)):

```go
// A Spec describes a custom guard for [New].
type Spec struct {
	Name     string                                        // lowercase snake_case
	Statuses []int                                         // for the OpenAPI document
	Check    func(ctx context.Context, req Request) error  // nil allows the request
}
```

```go
// A Request is what a custom guard can read about the request before its
// input is parsed.
func (r Request) PathParam(name string) string
func (r Request) Header(name string) string
func (r Request) Query(name string) string
func (r Request) Operation() *huma.Operation
```

**This is the constraint to design around: a guard cannot see the body.** Path parameters, headers, the query string and the operation — that is all. It runs before Huma parses the input, which is exactly why an anonymous caller never learns what the body should have looked like, and it is why there is no way to reach the handler's input struct from here.

**What we build ourselves.** The guard, and the use case it asks.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/delivery/guards.go#accepting-guard -->

<!-- include examples/apps/plateful/internal/modules/orders/usecase/restaurant_accepting.go#restaurant-accepting -->

**What just happened.** Follow the consequence of "a guard cannot see the body", because it reached all the way into the URL design.

Placing an order is `POST /v1/restaurants/{restaurantId}/orders`, not `POST /v1/orders` with `{"restaurant_id": "..."}` in the body. If the restaurant were in the body, this guard could not exist at all — there would be nothing for `req.PathParam` to read. The API is shaped the way it is so that the check is possible.

The rest of the shape matters just as much:

- **The `Check` returns a domain error**, and the module's `Errors` maps it, so the guard's refusals are `restaurant_not_found` and `restaurant_not_accepting` — the same codes the handler would produce, from one mapping ([chapter 11](11-validation-and-errors.md)).
- **`Statuses` tells the OpenAPI document** which statuses this route can now answer, so the published contract knows about your guard.
- **The guard is not the rule.** `PlaceOrder` asks the same question again inside its transaction, where the answer cannot change underneath it. That duplication is the point.

> **Don't do this**: make a guard the only place a rule lives.
>
> ```go
> // The only check that the restaurant is open.
> acceptingRestaurant(svc),
> ```
>
> A guard protects one route. The job that places orders from a partner feed, the CLI command that replays a failed order, the second endpoint somebody adds next quarter — none of them pass through it. And a guard's answer is already stale by the time the handler starts: a restaurant can be suspended in the millisecond between the two.
>
> **Do this instead**: enforce the rule in the use case, where every caller reaches it and where the transaction holds the answer still — and *also* put it in a guard, as a cheap early refusal that keeps the traffic out. That is what Plateful does, and the comment in `guards.go` says so in as many words.

---

## Shape C — module middleware

**What we are doing.** Letting an operator stop all customer writes across the whole app with one setting.

**Why this shape.** It has nothing to do with who is asking. It applies to every route in a group, it should run as early as possible, and it should cost nothing when it is off. That is middleware, not a guard.

The distinction is worth stating once, plainly:

- **A guard** answers a question about the *caller* or the *route's subject*, runs after authentication, is declared per route, and shows up in the OpenAPI document and in `orb routes`.
- **Middleware** wraps a group of routes, runs *before* authentication and the guards, and knows nothing about the actor.

**What the framework already gives us.** `gorbital.Use(mw)` as a group option, `httpx.WriteProblem` so your middleware's refusal looks like every other refusal in the app, and the settings registry ([the middleware stack](../guides/middleware-stack.md), [guards and middleware](../guides/guards-and-middleware.md)).

**What we build ourselves.** A plain `func(http.Handler) http.Handler`.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/delivery/middleware.go#pause-ordering -->

It is attached to the customer-facing group when the routes are registered:

<!-- include examples/apps/plateful/internal/modules/orders/delivery/routes.go#customer-routes -->

and the setting it reads is declared in the module, with a description an operator will read in `/ops/settings` at a bad moment:

<!-- include examples/apps/plateful/internal/modules/orders/module.go#settings-and-flags -->

**What just happened.** `PauseOrdering` is standard `net/http` — no framework types in the signature at all — which means you can test it with `httptest` alone, and lift it into another app unchanged. It reads a runtime setting, so turning ordering off is a change in `/ops/settings`, not a deploy; and turning it back on is another, not a rollback.

Three deliberate details: it leaves safe methods alone, so customers can still read the orders they already have; it sets `Retry-After`, which is a promise a mobile client can act on instead of hammering; and it writes its problem with `httpx.WriteProblem`, so `ordering_paused` arrives as `application/problem+json` with a request ID like everything else — which is also why the module does *not* map that code in `Module.Errors`. The middleware answers it itself, before any of that machinery runs.

[Shelfie's chapter 3](../examples/shelfie/03-your-own-middleware.md) writes another one — a middleware that both refuses requests and puts a value into the context for the handlers behind it.

---

## Shape D — a use case with no route at all

**What we are doing.** Finding, every five minutes, the orders a kitchen accepted and has not delivered, and telling each restaurant about its own.

**Why this shape.** Nobody asks for it. There is no client, no request and no caller. But it is still a rule about orders, so it belongs with the other rules — not buried in a worker.

This is the shape people most often get wrong, so it is worth being blunt about the mistake: **a "function" does not have to be an endpoint.** When the only caller is a scheduled job, it is tempting to write the logic as a method on the worker. Do not. A worker is an adapter — the same kind of thing a handler is. Put the work in a use case and let the worker call it.

What you get for that:

- the logic can be called directly from a test, with no River, no job row and no scheduler;
- a second caller (an ops endpoint, a CLI command, a different schedule) can reuse it;
- it sits beside the other operations on orders, where somebody looking for "everything that reads orders" will find it.

**What the framework already gives us.** The job system: definitions, schedules, timeouts, retries, and `/ops/jobs` for operators ([background jobs](../guides/background-jobs.md), [chapter 13](13-background-jobs.md)).

**What we build ourselves.** The use case, then a thin worker.

**How.** The use case, with no route anywhere near it:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/late_orders.go#late-orders -->

The job's name and arguments:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/late_sweep.go#late-sweep-args -->

The worker, which is as thin as a handler:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/late_sweep.go#late-sweep-work -->

And the definition that ties it to a schedule:

<!-- include examples/apps/plateful/internal/modules/orders/module.go#jobs -->

**What just happened.** Four details that generalise beyond this job.

**The time comes in as an argument.** `LateOrders` takes `now time.Time` rather than calling the clock. The job passes the moment it started; a test passes a time it chose. Any use case whose behaviour depends on the current time should do this.

**It reads across organisations**, which no request ever does — the sweep is the platform's, not a tenant's. That is a privilege, so it is confined to one method (`SelectLateOrders`) whose comment says exactly that.

**How late is "late" is a runtime setting**, read when the job runs. An operator changes `orders.late_after` and the next sweep uses the new value.

**`Deps.Jobs` is nil while jobs are being defined**, because the client is built *from* these definitions. So the worker takes what it needs from `Deps` — the pool, the logger, a service — and the notifier it is handed (`notifications.FromWorker`) takes the job client from the context when the job actually runs. The service built here has no transaction manager for the same reason; the sweep only reads. That is a real ordering constraint in the framework, and the comment in `module.go` names it rather than leaving you to discover it.

---

## Testing the three that touch HTTP

**What we are doing.** Driving the guard and the middleware through the real stack.

**Why.** A guard and a middleware are only as good as their position in the chain, and position is exactly what a unit test cannot check.

**What the framework already gives us.** `gorbitaltest`, with a real database, real sign-in and the app's real middleware stack ([testing with gorbitaltest](../guides/testing-with-gorbitaltest.md)).

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-guard-and-middleware -->

**What just happened.** The first test suspends a restaurant through the platform's own route and then watches the guard refuse the next order with `409 restaurant_not_accepting` — no cache to invalidate, no deploy, the suspension takes effect the moment it commits. A restaurant that does not exist answers the same way as one this caller may not order from.

The second proves the middleware's three properties at once: writes are refused with `503 ordering_paused` while the setting is on; **reads are untouched**, so customers can still see their orders and staff can still see the kitchen's; and flipping the setting back restores service immediately.

---

## Testing the one that does not

A job's worker is a struct with a method. Build it and call `Work`:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-late-sweep -->

**What just happened.** Workers do not run in tests, so the job rows are what there is to see — `App.Jobs(t, kind)` reads them. The test sweeps with nothing late and asserts nothing was enqueued, moves an order into the past, sweeps again, and asserts one notification appeared.

Then it does the thing this whole section is about: it calls `svc.LateOrders` **directly**, with a time of its choosing, and checks the report. No job, no worker, no River. That is only possible because the work is in a use case — and it is why the answer to "where do I put this?" is almost never "on the worker".

---

## Which shape do I want?

Five questions, in order:

1. **Does a client ask for it?** No → shape D, a use case with no route, called by a job or a command.
2. **Is the answer about who is calling, or about something the *path, headers or query* name?** Yes → a guard (shape B). Remember it cannot see the body; if the thing you need to check is in the body, either move it into the path or accept that the check belongs in the use case.
3. **Does it apply to a whole group regardless of the caller, and should it run before authentication?** Yes → middleware (shape C).
4. **Otherwise** → a use case with a thin handler (shape A).
5. **And whatever you chose:** if it is a rule that must always hold, put it in the use case *as well*. Guards and middleware make refusals cheap and visible; they are not where a rule lives.

One more thing that is true of all four: **none of them belong in a handler.** A handler reads the request, calls one function, and shapes the answer. Every handler in Plateful is four lines long, and that is not an accident — it is what makes the rules findable, reusable and testable.

---

## What to take from this chapter

- **An operation with its own rules is a use case**, with a handler thin enough to be uninteresting.
- **`guard.New` gives you a custom refusal** that is mapped like your other errors and appears in the OpenAPI document — but `guard.Request` sees only the path, headers and query, never the body, and that constraint should shape your URLs.
- **A guard is never the only place a rule lives.** It is the cheap early refusal; the use case is the enforcement.
- **Module middleware is plain `net/http`**, runs before authentication, applies to a group, and is right for anything that has nothing to do with who is asking.
- **A function does not have to be an endpoint.** When the only caller is a job, the logic still goes in `usecase`, and the worker stays as thin as a handler.

[Chapter 13](13-background-jobs.md) takes the job this chapter defined and follows it through the queue: schedules, retries, timeouts, and what an operator can do with it at three in the morning.
