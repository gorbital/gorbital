# 16. Reviews, and the one route that needs nobody

A customer gets their dinner, and half an hour later wants to say something about it. One review per order, editable for a day, visible to anyone deciding where to eat tonight. Platform staff can hide an abusive one; the restaurant it is about cannot.

That is a small feature with an awkward shape, and it is the reason it comes last among Plateful's ordinary modules. Every other module in the app has fitted one of two patterns:

| Shape | Written by | Row belongs to | Read by | Example |
|---|---|---|---|---|
| Organisation data | A member | An organisation | Its members | The restaurant profile, the menu |
| Caller's own data | The caller | The caller | The caller and the restaurant | An order |
| **A review** | **Somebody in no organisation** | **An organisation** | **Anybody at all** | This chapter |

A review is written by a non-member, about a row that belongs to a tenant, and read by people who are not signed in. Three different answers to "who is this for", in one table.

## 1. Writing one

**What we're doing.** `POST /v1/orders/{orderId}/review`.

**Why.** The order is the proof. Anyone can have an opinion about a restaurant; only the person a specific dinner was delivered to gets to leave a review of it, exactly once.

**What the framework already gives us.** A route, a permission check, and an authenticated actor in the context. That is all it can give, and the reason is worth stating plainly: the caller is a customer, a member of no organisation, so `guard.OrgMember` has nothing to ask about them, and the platform permission on the route is held by the `user` role — which every signed-in account has. **The guard authorises nothing here.** It establishes that somebody is signed in.

**What we build ourselves.** The authorisation, as a fact about data:

<!-- include examples/apps/plateful/internal/modules/reviews/usecase/write_review.go#write-review -->

**What just happened.** The order was read, checked to name this account as its customer, and checked to be delivered — **in that order**, which is the part worth copying. Ownership first, state second. An order belonging to somebody else answers `ErrOrderNotFound`, identically to an order that does not exist. Reverse the two checks and a stranger trying IDs learns which of them are delivered orders and which are nothing; the error message becomes an oracle.

The rating went up in the same transaction as the review. That is step 3.

## 2. A day to change your mind

**What we're doing.** `PATCH /v1/reviews/{id}`, for 24 hours.

**Why.** People write reviews annoyed and regret them, and people leave out the one detail that would have been useful. An unlimited edit window is a different feature: it means a review's text can change long after people have acted on it, and it means a restaurant that offers a refund can quietly get the review rewritten.

**What the framework already gives us.** Nothing. This is a domain rule.

**What we build ourselves.** One comparison, in the domain layer:

<!-- include examples/apps/plateful/internal/modules/reviews/domain/review.go#review-edit -->

and the use case around it:

<!-- include examples/apps/plateful/internal/modules/reviews/usecase/update_review.go#update-review -->

**What just happened.** The window is measured from `CreatedAt`, **not** `UpdatedAt`. Measuring from the last change would let a review be edited forever, one edit per day — which is the bug you get for free by writing `now.Sub(r.UpdatedAt)`, and which no test finds unless somebody thought about it.

Three refusals live side by side and mean three different things: `review_not_found` (it is not yours, or it does not exist — the same answer, so reviews cannot be enumerated), `review_window_closed` (it is yours, but it is now history), and `review_version_conflict` (it is yours and editable, but somebody's change landed first).

## 3. The rating is a count and a sum

**What we're doing.** Keeping what a restaurant's visible reviews add up to.

**Why.** The public list shows an average. Recomputing it with `AVG(rating)` on every page load is a table scan per visitor on the one route strangers can hit.

**What the framework already gives us.** Nothing; this is ordinary SQL in the module's repository.

**What we build ourselves.** A row per restaurant holding a count and a sum, in **this module's** table:

<!-- include examples/apps/plateful/internal/modules/reviews/domain/review.go#restaurant-rating -->

<!-- include examples/apps/plateful/internal/modules/reviews/repository/upsert_rating.go#rating-upsert -->

**What just happened.** Three decisions, each of which had an obvious-looking alternative.

**The total is in `restaurant_ratings`, not in a `rating` column on the restaurant's row in `orgs`.** It is derived entirely from reviews, and the module that owns the rows owns what is computed from them. Writing to the restaurant's columns would make this module a second writer of another module's data — and it would mean a restaurant row is locked by every review anybody leaves. This is the same boundary [chapter 15](15-payments-a-rule-across-modules.md) had to cross with SQL, and here we simply don't cross it.

**It is recomputed in the transaction that writes the review.** So the number a diner reads never disagrees with the rows on the same page. The alternative is a job that re-adds the column every few minutes: a smaller transaction and one fewer row to lock, at the cost of an average that is briefly wrong. A busier platform would take that trade. Plateful does not, because a restaurant with four reviews shows a visibly wrong average for as long as the job is behind.

**It stores the count and the sum, not the average.** Dividing two integers at read time is exact, and it means hiding a review can be undone by adding its stars back. Neither is true of a stored mean, which rounds once per review and drifts.

The deltas are applied by PostgreSQL (`review_count + $3`), not read into Go, changed and written back, so two reviews landing at the same instant both count.

## 4. The one public route

**What we're doing.** `GET /v1/restaurants/{restaurantId}/reviews`, with no sign-in.

**Why.** The person reading a restaurant's reviews is deciding where to eat and has not signed in — and will not sign in to read a review list. They will go to a competitor's app. Requiring authentication here would mean nobody reads the reviews, which means nobody writes them, which means the feature does not exist.

**What the framework already gives us.** A default, and one way to override it.

**gorbital's routes are deny-by-default.** From the `guard` package's own documentation: *every route requires an authenticated actor unless it has `Public`*. You do not add authentication to a route; it is there, and `guard.Public()` is how you take it away. The router will not even register an authenticated route if the API declares no bearer security scheme — it fails at startup with a message telling you to add the scheme or mark the route `guard.Public()`. There is no path by which a route quietly ends up open because somebody forgot a line.

That default is the reason this chapter can make a fuss about one route. In a framework where routes are open until secured, "which endpoints are public?" is a question you answer by auditing every file. Here it is one grep for `guard.Public()`, and in the whole of Plateful it finds two: the payment provider's webhook, which has no session but is signed, and this one.

**What we build ourselves.** The exception, and everything that follows from it:

<!-- include examples/apps/plateful/internal/modules/reviews/delivery/routes.go#review-routes -->

<!-- include examples/apps/plateful/internal/modules/reviews/usecase/list_reviews.go#list-reviews -->

**What just happened.** Four consequences, and each one is work the other routes did not need.

**A rate limit, keyed by IP.** This is the only endpoint an anonymous stranger can spend the platform's database on, and there is no user to key a budget by. `guard.RateLimit(120, time.Minute, guard.ByIP(), guard.Named("reviews_public_list"))` gives every instance of the route one shared budget, visible to operators in `/ops/auth/rate-limits`. (Named limiters created by `guard.RateLimit` are not also declared in `Module.RateLimiters` — gorbital refuses a module-declared limiter whose name a guard also uses, because their budgets would mix.)

**No actor call, anywhere.** `ListReviews` is the one operation in Plateful with no `callerID(ctx)` above it. That absence is the design, not an oversight, and writing it down in the doc comment is what stops somebody "fixing" it later.

**Hidden reviews are filtered in the query.** Not after it. A moderated review is absent from the page, absent from the count and absent from the average — because the count and the average come from `restaurant_ratings`, which step 3 keeps in step with the visible rows.

**The customer's ID never leaves the repository.** The response type has no field for it. A diner learns what was said and not who said it. On an authenticated route you might get away with returning an account ID that a client happens not to render; on a route anybody can call with `curl`, every field in the response is published.

## 5. Moderation, and a route that deliberately doesn't exist

**What we're doing.** `POST /v1/platform/reviews/{id}/hide`, for platform staff only.

**Why.** Reviews attract abuse, and somebody has to be able to take a review down. The interesting question is *who*.

**What the framework already gives us.** Two permission catalogues. A permission is declared with `OrgRoles` (held inside an organisation, through membership) or with `Roles` (a platform role). Both of this module's permissions use `Roles` and neither has `OrgRoles`:

```go
Permissions: []gorbital.Permission{
    {Name: usecase.PermWrite, Description: "Review an order delivered to you", Roles: []string{"user"}},
    {Name: usecase.PermModerate, Description: "Hide an abusive review", Roles: []string{"platform_admin"}},
},
```

That is the module in two lines: **nobody acting inside the restaurant's organisation may write, change or hide a review of it.**

**What we build ourselves.** The operation, and — more importantly — the absence beside it:

<!-- include examples/apps/plateful/internal/modules/reviews/usecase/hide_review.go#hide-review -->

**What just happened.** A restaurant owner reading their worst review has no route. Not a 403: no endpoint at all. The path is `/v1/platform/reviews/{id}/hide` and carries no organisation, because the operation is not a tenant's.

A product decision expressed as a missing route is invisible. Nothing in a route table says "and deliberately not this one", nobody reviewing a pull request that adds `/v1/orgs/{orgId}/reviews/{id}/hide` will remember that it was considered and refused, and six months later it looks like an oversight somebody helpfully filled in. So it is written down twice — in the route table's comment and in the use case's — in the places somebody adding that route would already be reading.

The restaurant's remedy is to ask platform staff, who leave an audit event naming themselves and their reason. That trail is the point: moderation that cannot be inspected is indistinguishable from a tenant deleting its own bad reviews. [Chapter 18](18-audit-logs-and-observability.md) is where those events get read back.

Hiding is idempotent, and for a specific reason: `Hide` reports whether anything changed, so a review hidden twice is counted out of the rating **once**. A second call finds nothing to change and subtracts nothing. Without that, two moderators clicking the same button would leave a restaurant's rating one review short.

## Where to go next

- The guards each of these four routes uses, and how to write your own: [guards and middleware](../guides/guards-and-middleware.md).
- Rate limiters, named budgets and what operators can do with them: [ops API reference](../guides/ops-api.md#authentication).
- Cursor pagination, which the public list uses and which is unsigned on purpose: [modules and routes](../guides/modules-and-routes.md).
- Next chapter: [extending the framework](17-extending-the-framework.md) — building something gorbital does not have, and owning the security that comes with it.
