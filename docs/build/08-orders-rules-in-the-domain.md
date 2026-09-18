# 8. Orders, and rules that live in the domain

An order is the only thing in Plateful that changes over and over. A dish is created and edited; an order is *placed*, then accepted or rejected, then prepared, then ready, then collected, then delivered — or cancelled somewhere along the way. Six different people can be pushing it along at once: the diner on their phone, two people in the kitchen, a courier, and support.

That is exactly the situation in which "just update the status field" goes wrong. This chapter builds the rules about what may happen to an order as a small, readable thing in one file, and then shows every part of the app asking it rather than re-deciding it.

Everything is in `examples/apps/plateful/internal/modules/orders`.

---

## 1. Where an order can be

**What we are doing.** Naming the states an order can be in, and writing down which moves between them are legal.

**Why.** Because the alternative is that the answer to "can this order be cancelled?" is spread across six handlers, and the sixth one disagrees with the other five. A double-tap on *Accept* on a busy Friday, a courier tapping *Delivered* on the wrong order, a customer cancelling something already in the oven — these are not exotic; they happen daily.

**What the framework already gives us.** Nothing here. gorbital has no state-machine helper and does not want one: which statuses your orders have is the most domain-specific thing in your app. What it gives us is the *place* — a `domain` package that imports only the standard library — and the error mapping that turns one refusal into one HTTP status ([chapter 11](11-validation-and-errors.md)).

**What we build ourselves.** A `Status` type, a table of legal moves, and three small methods over it.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/domain/order.go#order-status -->

**What just happened.** The machine is now one thing you can read in twenty seconds, and anything missing from `transitions` is refused. Note what the table encodes without a word of prose: a placed order can be accepted, rejected or cancelled; an accepted or preparing one can still be cancelled; a *ready* one cannot, because the food is made; `delivered`, `rejected` and `cancelled` have no entry at all, so they are terminal, and `Terminal()` is simply "nothing leads out of here".

---

## 2. Why this lives in `domain` and not in the handlers

**What we are doing.** Putting the rules one layer below the code that speaks HTTP.

**Why.** Three reasons, in increasing order of importance.

1. **Other callers.** The `orders_late_sweep` job ([chapter 12](12-your-own-function.md), [chapter 13](13-background-jobs.md)) reads orders. A support command might reject one. A rule in a handler exists only for people who arrive by that route.
2. **Testing.** `domain` imports only the standard library. Its tests need no database, no HTTP, no fixtures, and run in milliseconds — which means you write more of them, and the nasty cases get covered.
3. **Reading it back.** Six months from now the question "when can an order be cancelled?" has one answer, in one file, and it is short enough to check by reading it.

**What the framework already gives us.** The four-layer module layout — `domain`, `usecase`, `repository`, `delivery` — which `orb gen module` scaffolds, and an architecture test in the app that fails if a layer imports the wrong thing. [Modules and routes](../guides/modules-and-routes.md) is the reference.

**What we build ourselves.** The discipline of putting each rule at the layer that owns it:

| Question | Layer |
|---|---|
| Is this JSON well-formed, and is `quantity` between 1 and 99? | schema tags on the handler's input struct |
| Can an order in `ready` move to `cancelled`? | `domain` |
| Is this caller allowed to move *this* order? | `usecase` |
| Has the payment been authorised? | `usecase` (it needs another module's table) |
| Can two orders take the last portion? | `repository` and the database |

[Chapter 11](11-validation-and-errors.md) walks that table again from the validation side.

---

## 3. One function moves an order

**What we are doing.** Writing the single method that every status change goes through.

**Why.** Because "the rules are in the domain" is worth very little if there are five ways into the domain. There is one.

**What the framework already gives us.** Nothing — but note the shape it *encourages*: `MoveTo` returns a new `Order` rather than mutating one, so a refused move leaves the caller holding the order it started with, and there is no half-changed value to accidentally save.

**What we build ourselves.**

<!-- include examples/apps/plateful/internal/modules/orders/domain/order.go#order-move -->

**What just happened.** Read the last line of that doc comment again, because it is the division of labour the whole application is built on:

> A handler decides who is asking; the order decides whether what they ask is possible.

`MoveTo` does not know whether a restaurant, a customer or a courier is calling. It could not care less. It knows that an order in `ready` does not become `cancelled`, and it stamps `CollectedAt` when the order is collected so that nobody has to remember to. Everything about *who* is [chapter 10](10-who-may-see-this-row.md).

> **Don't do this**
>
> ```go
> func (h handlers) markReady(ctx context.Context, in *orgOrderIDInput) (*orderOutput, error) {
>     order, _ := h.store.Get(ctx, in.ID)
>     if order.Status == "cancelled" || order.Status == "delivered" {
>         return nil, huma.Error409Conflict("too late")
>     }
>     order.Status = "ready"
>     order.ReadyAt = time.Now()
>     return h.store.Save(ctx, order)
> }
> ```
>
> Every handler that moves an order now carries its own copy of a slightly different rule. The one that forgets `rejected` is the one that lets a rejected order be marked ready, and you will find it from a customer complaint.
>
> **Do this instead**: one `MoveTo` on the order, one `move` in the use case that calls it, and handlers that name a status and nothing else.

---

## 4. Proving it

**What we are doing.** Walking the happy path, then trying every move the machine forbids.

**Why.** A state machine is a table, and a table is exactly the kind of thing a test can cover completely and cheaply.

**What the framework already gives us.** Nothing: this is `go test` over a package with no dependencies. That is the point.

**What we build ourselves.**

<!-- include examples/apps/plateful/internal/modules/orders/domain/order_test.go#test-state-machine -->

**What just happened.** Every illegal move returns the same error, `ErrInvalidTransition`, which `module.go` maps to `409 invalid_order_transition`. So the second tap on *Accept* is a conflict rather than a second acceptance; a courier cannot deliver food the kitchen has not finished; a customer cannot cancel a meal that is boxed and waiting. Notice how little ceremony this test needs — no database, no HTTP, no fixtures. That is what "the rules live in `domain`" buys.

---

## 5. One path through the use case

**What we are doing.** Calling `MoveTo` from exactly one place per kind of caller, with the row locked.

**Why.** The domain says whether a move is *possible*. Something still has to read the current order, apply the move, and save it — and if that sequence is written six times, the sixth one forgets the lock.

**What the framework already gives us.** `postgres.InTx` and, through our own `TxManager` port, a transaction that several writes share ([chapter 9](09-one-transaction.md) is entirely about this).

**What we build ourselves.**

<!-- include examples/apps/plateful/internal/modules/orders/usecase/move_order.go#move-order -->

Each operation is then that path plus whatever extra rule it has. Accepting an order has one, and it is a good example of a rule that does not fit tidily anywhere:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/accept_order.go#accept-order -->

And the handler behind it:

<!-- include examples/apps/plateful/internal/modules/orders/delivery/advance_order.go#accept-handler -->

**What just happened.** `AcceptOrder` is not "update with a different name". Two rules decide it: the state machine's, and one that spans two modules — the money lives in the payments module's `order_payments` table, and a kitchen must not start cooking before the provider's webhook has written "authorised" there. The orders module reads that with SQL, inside the same transaction that locked the order, because a module never imports another module's layers.

Be honest about this shape rather than hiding it: gorbital has no way to express a rule that lives half in one module and half in another. The comment in `accept_order.go` and the comment on `ErrPaymentNotAuthorised` both say where the other half is. That is the available fix, and it is better than pretending the rule belongs to one module.

---

## 6. The customer's move

**What we are doing.** Letting a diner cancel.

**Why.** To show a second caller reaching the same machine and needing something extra that has nothing to do with the machine.

**What the framework already gives us.** `guard.Permission` on the route, which proves somebody is signed in.

**What we build ourselves.** The comparison that proves this order is theirs.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/usecase/cancel_order.go#cancel-order -->

**What just happened.** Two different questions were answered in two different places. *Can this order be cancelled at all?* — `MoveTo`, in the domain, for everyone. *Is this the person who placed it?* — a Go comparison, here, because a customer belongs to no organisation and so no guard can answer it. Somebody else's order returns `ErrOrderNotFound` rather than a `403`, so an ID cannot be probed to learn that an order exists.

That comparison, and the fact that nothing in the route table or the OpenAPI document mentions it, is what [chapter 10](10-who-may-see-this-row.md) is about. Read it next; it matters more than this chapter.

---

## 7. Lines snapshot the name and the price

**What we are doing.** Copying the dish's name and price onto the order when it is placed, instead of pointing at the menu.

**Why.** This is the second thing in the guide that looks like duplication and is not.

A restaurant edits its menu constantly: it renames "Margherita" to "Margherita (classic)", it puts the pizza up by 50p in March, it deletes the dish that never sold. If an order rendered itself by joining to `menu_items`, then every one of those edits would silently rewrite history:

- a receipt printed today for an order from March would show March's dish at today's price;
- the customer's order list would show a total that does not match what their card was charged;
- deleting a dish would break — or, worse, blank — every past order that contained it;
- a dispute six months later would have no record of what was actually sold.

An order is a record of a transaction that already happened. Records do not change when the catalogue does.

**What the framework already gives us.** Nothing to do it for us, and nothing in the way.

**What we build ourselves.** A `Line` that carries what was bought, and a table that stores it:

<!-- include examples/apps/plateful/internal/modules/orders/domain/order.go#order-line -->

<!-- include examples/apps/plateful/db/migrations/20260918010050_orders.sql#order-lines-table -->

The total is computed once, when the order is placed, and never recomputed from the menu afterwards — in the same integer minor units [chapter 7](07-the-menu-money-and-photos.md) insisted on:

<!-- include examples/apps/plateful/internal/modules/orders/domain/order_test.go#test-total-is-exact -->

**What just happened.** `item_id` is kept, because the restaurant wants to know which dish sold, but it is deliberately **not** a foreign key: deleting a dish must not delete history. The lines are written by the same transaction that writes the order ([chapter 9](09-one-transaction.md)), so an order never exists without the dishes it is for.

> **Don't do this**
>
> ```sql
> SELECT o.*, m.name, m.price_minor
> FROM orders o
> JOIN order_lines l ON l.order_id = o.id
> JOIN menu_items m ON m.id = l.item_id;   -- the menu as it is today
> ```
>
> **Do this instead**: store `name` and `price_minor` on the line at the moment of the order, and read them from there forever. The same reasoning applies to a delivery address, a tax rate, or anything else that a receipt has to be able to reproduce.

---

## 8. What the client sees

**What we are doing.** Shaping an order for the API.

**Why.** The response is a contract. Statuses are values clients write `switch` statements on.

**What the framework already gives us.** Huma generates the OpenAPI schema from the struct and its tags, including the `enum` of statuses, so the documented contract cannot drift from the code that produces it ([the OpenAPI module](../methods/modules-openapi.md)).

**What we build ourselves.** One response type, used by all three kinds of caller.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/delivery/responses.go#order-response -->

**What just happened.** One shape for a restaurant, a customer and a courier. Which orders each of them may read is decided in the use cases — *not* by hiding fields from some callers and showing them to others. Hiding fields in a serializer is where authorisation bugs go to live: the field comes back the moment somebody adds a new endpoint, or a `?fields=` parameter, or an export.

Two smaller details worth copying: the timestamps are pointers, so a step the order has not taken is absent from the JSON rather than claiming the year 1; and `price_minor` and `total_minor` are integers with `example` values that make the units unmistakable.

---

## What to take from this chapter

- **Write the state machine down as data**, in one table, in `domain`. Anything not in the table is refused.
- **One function performs a move**; one path in the use case calls it with the row locked.
- **A handler decides who is asking; the domain decides whether what they ask is possible.**
- **Test the machine directly.** It needs no database, so cover the illegal moves exhaustively.
- **Snapshot onto the order anything a receipt must be able to reproduce** — the dish's name, its price, the address. An order is history, and history does not follow the catalogue.

[Chapter 9](09-one-transaction.md) places an order: four writes and one background job that all have to happen together, or not at all.
