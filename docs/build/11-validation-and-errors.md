# 11. Validation, and what the client is told

Three chapters have now refused things: a dish with a negative price, an order at a closed restaurant, a status change the machine does not allow, an image larger than the limit, an order that is not yours. This chapter follows those refusals from the layer that raises them to the JSON a client parses, and answers the two questions that come up every time you add an endpoint:

- **where does this check belong?**
- **how does the client find out?**

The second one has a single answer in gorbital: a sentinel error, declared in `domain`, mapped in `Module.Errors`, written as `application/problem+json`. [The error handling guide](../guides/error-handling.md) is the reference; this chapter is the application of it.

---

## 1. Four places a check can live

**What we are doing.** Sorting the checks in Plateful by the layer that owns them.

**Why.** "Validate your input" is not one job. A length limit, a business rule and a uniqueness constraint are three different things with three different enforcement points, and putting one at the wrong layer either weakens it or makes it unreachable.

**What the framework already gives us.** The first layer, entirely free. Huma reads the tags on a handler's input struct and validates every request against the generated schema *before* your handler runs, and the same schema is what the OpenAPI document publishes — so the documentation cannot drift from the enforcement.

**What we build ourselves.** The other three.

**How.** In Plateful, a menu item's name is checked in four places, and each of them is doing something the others cannot:

| Layer | What it checks | What it is good at | What it cannot do |
|---|---|---|---|
| **Schema tags** (`delivery`) | `minLength:"1" maxLength:"100"` | refusing malformed requests cheaply, and documenting itself | anything that needs the database, or another field |
| **`domain`** | the name is valid text, trimmed, and not just whitespace | rules about what a *thing* is; testable with no database | anything that needs other rows |
| **`usecase`** | the caller may do this at all | rules about an *operation*, and about who is asking | enforcing a race-free constraint |
| **The database** | `CHECK (char_length(name) BETWEEN 1 AND 100)`, `UNIQUE (org_id, lower(name))` | being the last word, under concurrency, forever | producing a good error message |

**What just happened.** Read the right-hand column. Every layer has something it *cannot* do, which is why the checks are not redundant. The schema cannot know whether the name is taken. The domain cannot know either. The use case can ask, but between its question and its insert another request can slip in. Only the unique index is safe from that — and only the domain can say "is required" in words a person understands.

The rule of thumb: **check it as early as you can, and enforce it as late as you must.**

---

## 2. The edge: schema tags

**What we are doing.** Letting the generated schema refuse malformed requests.

**Why.** A request with `"quantity": -3` should never reach a use case, and a client should be able to learn that from the OpenAPI document without sending one.

**What the framework already gives us.** Everything. The tags are Huma's; gorbital wires them to the route and puts the resulting schema in `api/openapi.json`.

**What we build ourselves.** The tags, and the judgement about which limits belong here.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/delivery/place_order.go#place-order-handler -->

**What just happened.** `minItems:"1" maxItems:"50"`, `minimum:"1" maximum:"99"`, `maxLength:"500"` — a basket with no items, a quantity of zero or a novel-length note is refused before the handler body runs, with a `422` that names the offending field, and every one of those limits is in the published schema.

Two things to notice. First, the limits match the domain's constants (`MaxLines`, `MaxQuantity`, `MaxNoteLength`) and the migration's `CHECK` constraints: they are the same numbers said three times, on purpose, at three enforcement points. Second, `content_type` on the image upload has an `enum` of accepted media types — and the module still checks the type again after the upload, because the tag constrains what the client *claims*, not what it does ([chapter 7](07-the-menu-money-and-photos.md)).

> **Don't do this**: rely on schema tags alone for anything that matters. A tag protects one route. The same use case called by a job, a command, or a second endpoint with a slightly different input struct gets nothing.
>
> **Do this instead**: use tags as the cheap first refusal, and put the real rule in `domain` where every caller reaches it.

---

## 3. The middle: rules in `domain`

**What we are doing.** Validating the thing itself.

**Why.** Because "what a valid order looks like" is a property of an order, not of an HTTP request.

**What the framework already gives us.** Nothing, deliberately: `domain` imports only the standard library.

**What we build ourselves.** A validation that collects *every* problem rather than stopping at the first, and an error type that carries them.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/domain/errors.go#validation-error -->

**What just happened.** Two details carry the whole design.

`Order.validate` appends to a slice and returns all the field errors at once, so a form with three problems produces one response listing three problems — not three round trips.

And `Unwrap` returns `ErrInvalidOrder`. That is what makes `errors.Is(err, domain.ErrInvalidOrder)` true for a `*ValidationError`, which in turn is what lets one line in `Module.Errors` map every validation failure in the module to `422 validation_failed`. A rich error type and a mappable sentinel, from one value.

---

## 4. The floor: constraints in the database

**What we are doing.** Letting PostgreSQL be the last word.

**Why.** Two reasons, and the second is the one people forget.

The first is concurrency: a `SELECT` that finds no dish called "Margherita", followed by an `INSERT`, is not atomic, and two requests a millisecond apart will both find nothing and both insert.

The second is *everything that is not your handler*. A migration, a backfill script, a psql session at 2am, a future version of your own code — none of them go through your `usecase` package. A `CHECK` constraint does not care how the row got there.

**What the framework already gives us.** `postgres.UniqueViolation` and `postgres.ForeignKeyViolation`, which pull the constraint name out of a pgx error so you can turn a specific violation into a specific domain error.

**What we build ourselves.** The constraints, and a small translation at the repository boundary.

**How.**

The migration adds `CREATE UNIQUE INDEX menu_items_org_name ON menu_items (org_id, lower(name))`, so one restaurant never sells two dishes of the same name, ignoring case. The repository turns that one violation into a domain error and leaves every other error alone:

<!-- include examples/apps/plateful/internal/modules/menus/repository/store.go#name-taken-constraint -->

The orders module does the same for a foreign key, and its comment says why it is worth doing at all:

<!-- include examples/apps/plateful/internal/modules/orders/repository/insert_order.go#insert-order -->

**What just happened.** A violation the use cases handle became a domain error — `ErrItemNameTaken`, `ErrRestaurantNotFound` — which the module maps to a `409` or a `404` with a code a client can act on. Everything else is returned as it is and ends up as a logged `500`, which is correct: an unexpected constraint violation is a bug, not a conversation to have with the client.

Note the translation happens in `repository`, at the one boundary where pgx errors exist. `usecase` never sees a driver error, and `domain` has never heard of a database.

---

## 5. The loop: sentinel → `Module.Errors` → problem+json

**What we are doing.** The mechanism, start to finish. This is the thing to internalise.

**Why.** Every module in Plateful does exactly this, and once you have seen it once, every module's `Errors` block reads as documentation.

**What the framework already gives us.** `httpx.Mapping`, the mapper, and the problem response ([`httpx`](../methods/httpx.md)):

```go
// A Mapping maps a sentinel error (matched with [errors.Is]) to an HTTP
// status and stable code. Detail defaults to the error's message.
type Mapping struct {
	Err    error
	Status int
	Code   string
	Detail string
}
```

**What we build ourselves.** The sentinels, and one `Errors` block per module.

**How.** It is four steps.

**One — declare the sentinel in `domain`, with a comment that says what it means:**

<!-- include examples/apps/plateful/internal/modules/orders/domain/errors.go#payment-error -->

**Two — return it from wherever the rule lives.** `AcceptOrder` returns `ErrPaymentNotAuthorised` from inside a transaction ([chapter 8](08-orders-rules-in-the-domain.md)); `postgres.InTx` returns it unchanged, which is what keeps step four working ([chapter 9](09-one-transaction.md)).

**Three — map it in the module, with a status and a stable code:**

<!-- include examples/apps/plateful/internal/modules/orders/module.go#errors -->

**Four — the client gets a problem document**, and the mapper fills in the request ID:

```json
{
  "title": "Conflict",
  "status": 409,
  "code": "payment_not_authorised",
  "detail": "the order's payment is not authorised yet",
  "request_id": "req_9f86d081884c7d65"
}
```

**What just happened.** Four facts about this loop are worth stating explicitly.

- **Matching is `errors.Is`.** A wrapped error still matches, so `fmt.Errorf("%w: %s", domain.ErrItemUnavailable, itemID)` maps correctly while carrying the dish's ID in the message. Use `%w`, never `%v`.
- **`code` is public API; `title` and `detail` are not.** Clients switch on the code. Rewriting a detail is a copy edit; changing a code breaks somebody's error handling. The app records its codes in `api/surface.json`, and a test fails when one disappears.
- **The mapper refuses duplicates and inconsistent statuses at startup.** Mapping the same error twice, or giving one code two statuses in two modules, fails the app's boot rather than producing an inconsistent API. Several modules may share the code `unauthenticated` because they all use `401` for it — and they all do.
- **`request_id` is in every problem.** It is the string to ask a user for, and the one to grep the logs by ([observability](../guides/observability.md)).

---

## 6. What a module deliberately does *not* map

**What we are doing.** Reading an `Errors` block for what is missing.

**Why.** Because the absences are where the framework is already answering, and mapping them again would produce two answers to one question.

**How.** Compare three modules:

<!-- include examples/apps/plateful/internal/modules/images/module.go#image-errors -->

<!-- include examples/apps/plateful/internal/modules/payments/module.go#payment-errors -->

<!-- include examples/apps/plateful/internal/modules/reviews/module.go#review-errors -->

**What just happened.** In every one of them, `org_not_found` and `forbidden` are absent — `guard.OrgMember` answers those itself, before any handler runs. The payments module does not map `invalid_webhook_signature`, because `guard.Webhook` answers it first. The orders module does not map `ordering_paused`, because its own middleware writes that problem directly ([chapter 12](12-your-own-function.md)).

The reviews block is worth reading as prose: it explains that two different refusals deliberately give the *same* answer — somebody else's order and a non-existent order are both `order_not_found` — for the reason [chapter 10](10-who-may-see-this-row.md) laboured. And the couriers module's block explains what it *cannot* say:

<!-- include examples/apps/plateful/internal/modules/couriers/module.go#courier-errors -->

A courier's own routes cannot answer `org_not_found`, the refusal every organisation-scoped route in this app leans on, because a courier has no organisation to not find.

---

## 7. Unmapped errors become a logged 500

**What we are doing.** Deciding what happens to an error nobody mapped.

**Why.** This is the default that makes the whole scheme safe to work with.

**What the framework already gives us.** Exactly this behaviour:

```go
// Problem returns the problem for err. Unmapped errors become 500
// "internal_error" with a generic detail and are logged.
```

**What we build ourselves.** A guard against leaking driver errors into the API by accident.

**How.** Every use case in Plateful funnels store errors through a small helper — the orders module's is `storeError`, which, as its comment says, returns the module's own errors as they are and hides the rest, such as driver errors, which are not API.

It checks the error against the module's known sentinels with `errors.Is`, returns it unchanged if it matches, and otherwise wraps it in a plain message that no mapping will match — so it becomes a `500`.

**What just happened.** The failure mode is the safe one. If you add a new sentinel and forget to map it, the client gets `500 internal_error` with a generic detail, and the *real* error is in the logs once, with the request ID. Nobody learns your table names, your constraint names or your connection string from a `500`, and you find out from the logs rather than from a customer.

There is one more level of safety in the same direction: the detail of a mapped error never echoes the input back. A `FieldError` says what is wrong, never what was sent — its own comment says so, *"It never echoes the submitted value, which may be a password or personal data."*

> **Don't do this**
>
> ```go
> if err != nil {
>     return nil, huma.Error500InternalServerError(err.Error())  // straight to the client
> }
> ```
>
> **Do this instead**: return your sentinel and let the mapper decide. If the error is genuinely unexpected, returning it *unmapped* is the right move — the framework logs it and tells the client nothing.

---

## 8. Field errors: `422` with a list

**What we are doing.** Turning a `*ValidationError` full of fields into a problem document with an `errors` array.

**Why.** A form with three bad fields should highlight three fields.

**What the framework already gives us.** `httpx.FieldError` and the `Errors` array on `Problem`:

```go
// FieldError describes one invalid input field. It never echoes the
// submitted value, which may be a password or personal data.
type FieldError struct {
	Location string `json:"location,omitempty" example:"body.name"`
	Message  string `json:"message" example:"expected length >= 1"`
}
```

Returning an `*httpx.Problem` from a handler is enough: the mapper matches it with `errors.As` and writes it as it is, so you can build a richer problem than a `Mapping` can express.

**What we build ourselves.** One helper per module, used by every handler that accepts a body.

**How.**

<!-- include examples/apps/plateful/internal/modules/orders/delivery/responses.go#field-errors -->

**What just happened.** `fieldErrors(err, "body")` is the only thing standing between the domain's field names (`items.0.quantity`) and the client's (`body.items.0.quantity`). If the error is not a `*ValidationError` it is passed through untouched, so the helper is safe to wrap every call in — which is what the handlers do — every one of them returns `fieldErrors(err, "body")` rather than the error itself, as the `placeOrder` handler above does.

The client receives:

```json
{
  "title": "Unprocessable Entity",
  "status": 422,
  "code": "validation_failed",
  "detail": "the order is not valid",
  "request_id": "req_9f86d081884c7d65",
  "errors": [
    {"location": "body.address", "message": "is required"},
    {"location": "body.items.0.quantity", "message": "must be between 1 and 99"}
  ]
}
```

The location prefix is a parameter (`"body"` or `"query"`) because the same domain error can arrive from either, and telling a client that `body.status` is wrong when they sent `?status=` is worse than saying nothing.

---

## 9. The whole thing on one request

A customer sends a basket with an empty address, one dish that has sold out, and a quantity of 0.

1. **Schema.** `quantity: 0` fails `minimum:"1"`. Huma answers `422` with a field error, and the handler never runs. The other two problems are not reported yet — the request never got far enough — which is the cost of refusing early, and worth it.
2. The client fixes the quantity and retries. **Domain.** The address is empty and the customer's profile has none either, so `Order.validate` returns a `*ValidationError` naming `address`. `Unwrap` makes it `ErrInvalidOrder`; the mapping makes it `422 validation_failed`; `fieldErrors` adds `body.address`.
3. The client fixes the address and retries. **Use case.** Inside the transaction the dish has no stock left: `ErrItemOutOfStock`, wrapped with the dish's ID. `postgres.InTx` rolls back and returns it unchanged, `storeError` recognises it, the mapping makes it `409 dish_out_of_stock`, and — as [chapter 9](09-one-transaction.md) proved — nothing was written and no job was enqueued.
4. The client shows "sorry, the olives have run out" because it switched on the `code`, not on the English.

---

## What to take from this chapter

- **Check early, enforce late.** Schema tags refuse malformed requests; `domain` says what a valid thing is; `usecase` says who may do it; the database has the last word under concurrency.
- **The same limit said at several layers is not duplication.** Each layer can do something the others cannot.
- **One sentinel, one mapping, one code.** Declare the error in `domain`, map it in `Module.Errors`, and let the client switch on `code`.
- **Matching is `errors.Is`, so wrap with `%w`** and never flatten an error with `%v` on its way up.
- **Unmapped errors become a logged `500`** — a safe default, and the reason to hide driver errors behind a generic message rather than mapping them.
- **Never echo the submitted value back** in a field error.

[Chapter 12](12-your-own-function.md) leaves the well-trodden path: how to add your own operation, your own guard, your own middleware — and a use case with no route at all.
