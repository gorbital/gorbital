# 10. Who may see this row

This is the most important chapter in the guide, and the one to reread.

Plateful has one `orders` table. Three completely different kinds of caller need to read rows from it:

- **a restaurant's staff**, who may see every order belonging to their restaurant;
- **a customer**, who may see the orders they placed — at restaurants they have nothing to do with;
- **a courier**, who may see the one order assigned to them, and nothing else.

One table, three rules, and only the first of them can be written in the route table. The other two are Go comparisons inside use cases, invisible to the router, to the OpenAPI document and to `api/surface.json`. That is not a flaw in the app; it is the shape of the problem, and it is what makes this the chapter worth reading twice.

Everything is in `examples/apps/plateful/internal/modules/orders`.

---

## 1. Two kinds of permission, and why that is the crux

**What we are doing.** Getting clear on what a permission is on this platform before we use one.

**Why.** Almost every mistake in this area comes from thinking a permission answers a question it does not answer.

**What the framework already gives us.** Two catalogues, and a permission belongs to exactly one of them ([authentication](../guides/authentication.md), [guards and middleware](../guides/guards-and-middleware.md)):

- an **organisation permission**, held through a role *within an organisation* (`owner`, `admin`, `member`), and checked by `guard.OrgMember(perm)`;
- a **platform permission**, held through a platform role, and checked by `guard.Permission(perm)`.

The platform roles in Plateful are `user`, `ops_viewer` and `platform_admin`. Every signed-in account holds `user`.

**What we build ourselves.** Five permission names, and the decision about which kind each one is:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/service.go#order-permissions -->

<!-- include examples/apps/plateful/internal/modules/orders/module.go#permissions -->

**What just happened.** Look at the last three. `orders.order.place`, `orders.order.view` and `orders.order.deliver` are granted to the role `user` — which **every signed-in account on the platform holds**. A diner has it. A courier has it. A restaurant owner has it. A support account has it. Somebody who signed up thirty seconds ago and has never ordered anything has it.

So a route carrying `guard.Permission("orders.order.place")` says exactly one thing: *somebody is signed in*. It is not a lie and it is not useless — it keeps anonymous callers out, and an API key only holds it when its scopes include it ([API keys](../guides/api-keys.md)) — but as an authorisation statement about a *row*, it is worth nothing at all.

And there is no better permission available. There is no `customer` role, because "customer" is not a role somebody is granted — it is a relationship between an account and a row. There is no `courier-of-order-ord_123` permission, and there could not be: permissions are a fixed catalogue declared at build time, and this one would have to be invented per order.

---

## 2. Shape one: the restaurant's staff

**What we are doing.** Letting a kitchen see its own orders.

**Why.** Start with the easy case, because it is the one the framework handles completely — and because it is the standard everything else is measured against.

**What the framework already gives us.** `guard.OrgMember(perm)`, which checks that the caller is a member of the organisation in the path *and* that their role there holds the permission, and answers `404 org_not_found` otherwise — as if the organisation did not exist, so you cannot discover which organisations are real by probing.

**What we build ourselves.** Almost nothing. The organisation goes into the query.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/delivery/routes.go#staff-routes -->

<!-- include examples/apps/plateful/internal/modules/orders/usecase/get_order.go#get-org-order -->

The query behind it is `SELECT … FROM orders WHERE org_id = $1 AND id = $2`. On Plateful `org_id` is also the restaurant: a restaurant is its organisation's row in `orgs` ([chapter 5](05-the-restaurants-module.md#a-restaurant-is-its-organisation-one-table)), so there is no `restaurant_id` beside it that could name a different tenant than the one the guard checked.

**What just happened.** The organisation is *part of the query*, not a check performed afterwards. Another restaurant's order is not filtered out of the result; it was never in it. The route declares the whole rule, and `orb routes` can print it, and the OpenAPI document records it as `org_member:orders.order.read`.

This is the shape to prefer whenever the caller has a tenant. A filter in a `WHERE` clause cannot be forgotten by the next person to touch the function; a comparison after the read can.

---

## 3. Shape two: the customer

**What we are doing.** Letting a diner see the orders they placed.

**Why.** This is where the easy shape stops working, and it is worth being precise about *why* rather than just working around it.

A customer orders from Trattoria Bruno. The order belongs to Bruno's organisation. The customer is not a member of that organisation — they are a member of no organisation at all. So:

- `guard.OrgMember` cannot let them in, because they fail the membership check by definition;
- putting customers into every restaurant's organisation to make the guard work would hand them the *staff* routes for that restaurant, which is catastrophically worse;
- the organisation-scoped path is the wrong door anyway. The restaurant's ID *is* its organisation's, so a diner does know it — it is in `/v1/restaurants/{id}` — but everything under `/v1/orgs/{orgId}/` is the staff's, guarded by membership. Knowing an ID is not a reason to be let through a door.

**What the framework already gives us.** `guard.Permission(usecase.PermView)` — "somebody is signed in and holds `orders.order.view`" — and `actor.From(ctx)`, which is the signed-in account.

**What we build ourselves.** The rule. All of it.

**How.** The routes:

<!-- include examples/apps/plateful/internal/modules/orders/delivery/routes.go#customer-routes -->

The helper that gets the caller, with its warning attached:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/service.go#caller-id -->

The query, which cannot be scoped:

<!-- include examples/apps/plateful/internal/modules/orders/repository/select_order.go#select-order -->

And the rule itself:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/get_order.go#get-order -->

**What just happened.** `SelectOrder` reads the order **across the whole platform** — it has to, because there is no tenant to scope it by — and then one `if` in `GetOrder` decides whether the caller may have it: unless the order's `CustomerID` is the caller, or `carriedBy` says the caller is the courier carrying it, the answer is `ErrOrderNotFound`.

That comparison is the entire authorisation rule for this route. Delete it and the endpoint returns any order in the database to any signed-in account.

The refusal is `ErrOrderNotFound`, not "forbidden", and that is deliberate:

<!-- include examples/apps/plateful/internal/modules/orders/domain/errors.go#not-found-error -->

A `403` would confirm that the order exists. A `404` for both "no such order" and "not yours" means an ID tells an attacker nothing.

---

## 4. Shape three: the courier

**What we are doing.** Letting a courier see and advance the one order they are carrying.

**Why.** It is a third rule again — not the tenant's, not the caller's own, but "the row names the courier profile belonging to this account".

**What the framework already gives us.** The same `guard.Permission`, this time with `orders.order.deliver` — which, again, every signed-in account holds.

**What we build ourselves.** Two steps: does this account have a courier profile at all, and does this order name it?

**How.** `carriedBy`, the second half of the `GetOrder` listing above: it refuses at once when nobody is carrying the order, and otherwise looks up the caller's courier profile and compares it with `order.CourierID`. The moving parts a courier's route has to advance an order are the same two, in `CourierAdvance`.

The couriers module makes the same point about its own routes, from the other side:

<!-- include examples/apps/plateful/internal/modules/couriers/usecase/service.go#courier-permissions -->

**What just happened.** A courier is a row in a **platform-scoped** table — `couriers` has no `org_id` at all, because a courier delivers for many restaurants. So there is no organisation anywhere in this rule. The account must have a courier profile (an account without one is `not_a_courier`), and the order must name that profile, or the answer is `order_not_found` like everything else.

Three shapes, side by side:

| Caller | Guard on the route | Where "which rows" is decided |
|---|---|---|
| Restaurant staff | `guard.OrgMember("orders.order.read")` | in the SQL: `WHERE org_id = $1` |
| Customer | `guard.Permission("orders.order.view")` | in Go: `order.CustomerID != caller` |
| Courier | `guard.Permission("orders.order.deliver")` | in Go: `order.CourierID != courierOf(caller)` |

The same three shapes govern the lists, and the same filter discipline applies there:

<!-- include examples/apps/plateful/internal/modules/orders/usecase/list_orders.go#three-lists -->

<!-- include examples/apps/plateful/internal/modules/orders/usecase/list_orders.go#list-orders -->

Note what that says about cursors: a cursor is opaque, but opaque is **not signed**. A client can decode one. If the organisation or the customer ID lived inside the cursor, a pasted cursor would be an authorisation bypass. The filter goes in the query, from the request, on every page; the cursor carries only a position.

---

## 5. Be honest: what the route table does not say

**What we are doing.** Saying plainly what this design costs, instead of implying the guards cover it.

**Why.** Because a reader who believes the route table is the authorisation model will eventually ship a route where it is not.

Here is what the app's generated artefacts say about `GET /v1/orders/{id}` — the route where a customer or a courier reads an order.

The OpenAPI document:

```json
"x-gorbital-guards": ["authenticated", "permission:orders.order.view"]
```

Compare the staff route, `GET /v1/orgs/{orgId}/orders/{id}`:

```json
"x-gorbital-guards": ["authenticated", "org_member:orders.order.read"]
```

And `api/surface.json`, which records the app's public names so a test fails when one changes, lists the permission under the platform catalogue:

```json
"platform": ["orders.order.deliver", "orders.order.place", "orders.order.view"]
```

Now say those out loud. The strongest thing the customer's route can *declare* is "a signed-in account holding `orders.order.view`", and `orders.order.view` is granted to `user`, which is every account. **Nothing in the route table, the OpenAPI document or the recorded surface distinguishes "your own order" from "any order".** The distinction exists only in `usecase/get_order.go`, in a comparison that no tool checks and no reviewer is forced to look at.

The consequence, stated without softening it:

> Every route of this shape is one forgotten comparison away from letting people read each other's data.

That is not hypothetical. It is the most common serious bug in multi-tenant APIs, and it looks like this in the diff:

```go
func (s *Service) GetOrder(ctx context.Context, id string) (domain.Order, error) {
	if _, err := callerID(ctx); err != nil {
		return domain.Order{}, err
	}
	return s.store.SelectOrder(ctx, id, false)   // …and nothing else
}
```

Nine lines shorter. Passes review if the reviewer is looking at the route table. Passes every test that only checks the happy path. Returns every order in the database to anybody with an account.

**What just happened.** We named the gap. Now, the three things that actually close it.

### Prefer the shape that cannot be forgotten

Where the caller has a tenant, put the tenant in the query. `SelectOrder`'s own doc comment says so:

> Prefer that shape wherever the caller has a tenant: a filter in the query can't be forgotten, and a comparison afterwards can.

Use the unscoped read only where there is genuinely no tenant to scope by — which, for a customer and a courier, there is not.

### Put the check where every caller reaches it

The comparison is in the **use case**, not the handler. A job, a CLI command or a second route that calls `GetOrder` gets the rule for free. A rule in a handler protects one route.

### Test it, because a test is what catches this

There is no compiler error for a missing comparison and no linter for it. A test is the only thing standing between you and that nine-line diff, so the test is not optional — it *is* the mechanism:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-three-shapes -->

Read what that test asserts, because it is a checklist worth copying for any route of this shape:

- the intended caller succeeds (staff, then the diner, then the courier once the order is theirs);
- **another tenant** gets `org_not_found`, and does not find the order in their own list either;
- **another signed-in account** gets `order_not_found` — the same answer as a non-existent ID;
- **an anonymous caller** gets `401`;
- the courier gets `order_not_found` **before** the order is assigned, and `200` after;
- **a different courier** gets `order_not_found`, for reading *and* for advancing;
- an account with no courier profile gets `not_a_courier`;
- the customer cannot use a courier's route on their own order.

If you write one test for a route of this shape, write that one. It is the difference between a rule and a hope.

A related trap has its own test, because it is easy to reintroduce:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-cursor-is-not-authorisation -->

---

## 6. The contrast: a route that really is public

**What we are doing.** Looking at the one endpoint in Plateful that needs no sign-in at all, to see how different a genuinely public route looks.

**Why.** Because `guard.Public()` is a deliberate, visible declaration — and the contrast with "a permission every account holds" is instructive. One of them says "anyone may read this"; the other says "a signed-in person may call this, and the rule is elsewhere". They are not the same statement, and they should not be confused.

**What the framework already gives us.** `guard.Public()`, the only way out of gorbital's deny-by-default; `guard.RateLimit(…, guard.ByIP())` for callers who have no user to count against; and `orb routes --public`, which lists every route that needs no sign-in — a list worth failing a build over when it grows unexpectedly.

**What we build ourselves.** The judgement about which routes deserve it.

**How.**

<!-- include examples/apps/plateful/internal/modules/reviews/delivery/routes.go#review-routes -->

**What just happened.** Four routes, four different kinds of caller, in one small module — and the public one is public *on purpose*, for a reason stated in the code: somebody choosing where to eat has not signed in yet, and requiring them to would mean nobody read the reviews. Because it is the one route an anonymous stranger can spend the platform's database on, it carries the module's one rate limit, keyed by IP because there is no user to key it by.

Notice also what is *absent*: there is no organisation-scoped route for hiding a review. A restaurant cannot hide its own bad reviews by any route at all, and the missing route is the product decision.

---

## 7. Reaching past a guard

**What we are doing.** One more failure mode, because it is the mirror image of the one above.

**Why.** The bugs in this area come in two shapes: forgetting the check, and *bypassing* the one you have.

> **Don't do this**
>
> ```go
> // A new "admin" route that reuses the customer's read, with the check
> // skipped because "ops staff should see everything".
> func (s *Service) GetAnyOrder(ctx context.Context, id string) (domain.Order, error) {
> 	return s.store.SelectOrder(ctx, id, false)
> }
> ```
>
> ```go
> // Or: a handler that fetches the actor itself and trusts a header.
> orgID := r.Header.Get("X-Org-Id")   // chosen by the caller
> orders, _ := s.store.SelectOrders(ctx, ListQuery{OrgID: orgID})
> ```
>
> The first one is now a second, unguarded door to every order, and the next person to need "get an order by ID" will find it and call it. The second takes the tenant from the request instead of from the guard, which is simply authorisation by request header.
>
> **Do this instead.** Take the identity from `actor.From(ctx)`, which the authentication middleware filled in and the caller cannot forge. Take the tenant from the path the guard checked — `memberID(ctx, orgID)` asserts `a.OrgID == orgID`, so a route that forgot `guard.OrgMember` fails closed rather than trusting the path. And if platform staff genuinely need to read any order, give that its own route under `/v1/platform/…` with a platform permission that only platform roles hold, the way the reviews module does for hiding a review — so it is visible in the route table, in `orb routes`, and in the audit log.

---

## What to take from this chapter

- **A permission is not ownership.** `guard.Permission("orders.order.place")` means "signed in", because `user` is granted to every account.
- **Where the caller has a tenant, put the tenant in the query.** `WHERE org_id = $1` cannot be forgotten; a comparison after the read can.
- **Where the caller has no tenant — customers, couriers, anyone in a relationship with a row rather than an organisation — the rule is a comparison in the use case**, and there is no way to declare it on the route.
- **Answer `404`, not `403`, for a row that is not yours**, so IDs cannot be probed.
- **Nothing in the route table, the OpenAPI document or `api/surface.json` records that comparison.** Every such route is one forgotten line away from a data leak, and the only thing that catches it is a test that tries the other people.
- **Write that test**: the intended caller, another tenant, another account, an anonymous caller, and the same checks on writes as on reads.

[Chapter 11](11-validation-and-errors.md) follows a refusal from the layer that raises it to the JSON the client reads.
