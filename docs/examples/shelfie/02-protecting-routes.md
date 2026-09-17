# 2. Protecting routes

[Chapter 1](01-books-module.md) put guards next to the routes they protect and moved on. This chapter reads them: what a route is protected by when it says nothing at all, what each guard adds, and what the client is told when one refuses. Every status and code below comes from a test of the books module ([Guards and middleware](../../guides/guards-and-middleware.md) is the reference).

## 1. Deny by default

The books route table, in full:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/routes.go#routes -->

The group's `gorbital.Use(RequireClientVersion)` and the `ActiveSubscription()` on the export are the module's own, and [chapter 3](03-your-own-middleware.md) writes them; everything else here is the library's.

Not one of these routes says who may call it in general, and that is the point: a route with no `guard.Public()` requires an authenticated caller. Forgetting a guard makes a route *harder* to reach, never easier. The refusal happens before Huma parses the request, so an anonymous caller never learns what the body should have looked like:

<!-- include examples/apps/shelfie/internal/modules/books/protection_test.go#deny-by-default -->

## 2. Permissions

`guard.Permission("books.book.read")` checks a permission the authentication middleware computed from the caller's roles. The module declares it so it exists in the catalogue, and names the roles that hold it:

<!-- include examples/apps/shelfie/internal/modules/books/module.go#permissions -->

Every signed-in reader holds the `user` role, so every reader can manage their own shelf. An API key holds a permission only when its scopes include it, which is how a reader hands out a key that reads their shelf without being able to empty it ([chapter 4](04-tests.md) tests that).

A permission is not ownership. `books.book.write` says a reader may write *books*, not *other people's* books; the repository's `WHERE owner_id = $1` says the rest, and Bob asking for Ada's book gets 404, not 403, so IDs can't be probed.

## 3. Rate limits

`guard.RateLimit(30, time.Minute)` on `POST /v1/books` gives each reader thirty new books a minute, in bursts of up to thirty:

<!-- include examples/apps/shelfie/internal/modules/books/protection_test.go#rate-limit -->

The budget is per caller, not per route: one reader hitting the limit doesn't slow anyone else down, and the reads on the same shelf are untouched. `guard.ByIP()` counts per client address instead, which is what [chapter 7](07-phone-code-sign-in.md)'s sign-in routes use, because a caller who hasn't signed in yet has no user to count. A limiter that can't decide at all allows the request: rate limits slow abuse down, they don't lock readers out.

## 4. A recent sign-in

`DELETE /v1/books` empties a shelf, and nothing keeps a copy. A permission is the wrong question for an operation like that, because a stolen session holds every permission its owner does; the question is whether the person is still at the keyboard. `guard.RecentReauth()` requires a session that signed in, or verified a second factor, in the last ten minutes:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/routes.go#empty-shelf-route -->

<!-- include examples/apps/shelfie/internal/modules/books/protection_test.go#recent-reauth -->

A client that gets `reauthentication_required` asks the person to sign in again and retries. An API key never can, so it gets `session_required` instead: this is an operation for a person, not for a script. The same guard belongs on anything that changes how an account signs in, or shows its recovery codes.

## 5. Public routes

`guard.Public()` is the only way out of the default, and it applies to a group as well as a route. Shelfie uses it twice, both in [chapter 7](07-phone-code-sign-in.md): a reader asking for a sign-in code, and sending one back, have no session yet by definition. Both carry `guard.RateLimit(…, guard.ByIP())` in the same breath, because a public route is the one place where the caller is whoever asked. [Chapter 10](10-hardening.md)'s partner webhook is public too, and pairs `guard.Public()` with `guard.Webhook`, which verifies the sender's signature on the raw body.

`orb routes --public` lists every route that needs no sign-in, which is worth failing a build over when it grows unexpectedly ([chapter 9](09-generators.md)).

## 6. What the client sees

Every refusal is `application/problem+json` with the same shape, and a `code` clients switch on:

| The caller | Status | `code` |
|---|---|---|
| Has no session | 401 | `unauthenticated` |
| Lacks the permission | 403 | `forbidden` |
| Is over the limit | 429 | `rate_limited`, with `Retry-After` |
| Signed in more than ten minutes ago | 403 | `reauthentication_required` |
| Is an API key, on a route needing a session | 403 | `session_required` |

Each guard also adds its responses to the route's OpenAPI operation and lists itself in `x-gorbital-guards`, so `/docs`, the generated clients and the Dev Portal's Routes screen show the same thing the code does:

```json
"x-gorbital-guards": ["authenticated", "permission:books.book.write", "rate_limit:30/1m0s"]
```

The guide's table covers the guards Shelfie doesn't use yet: `guard.OrgMember` arrives with [chapter 8](08-book-clubs.md)'s book clubs, and `guard.Webhook` with [chapter 10](10-hardening.md).

## 7. Where the refusals show up

Guards run in the order they're written, a group's first, and the first one to refuse ends the request: on `POST /v1/books`, a caller without `books.book.write` is told so without spending any of their rate-limit budget. Each refusal increments `gorbital.guard.refusals`, labelled with the guard, the module and the route, and sets `gorbital.guard.refused` on the request's span. That is worth a dashboard: a wave of `permission:…` refusals after a deploy usually means a role lost a permission, and a wave of `rate_limit:…` means a client is retrying in a loop.

There is no way to take a guard off one route of a group that has it. Routes that need something different go in their own group — which is exactly what [chapter 7](07-phone-code-sign-in.md)'s public sign-in routes do, next to the phone routes that require a session.

## Next

[3. Your own middleware and guards](03-your-own-middleware.md): a rule of the module's own, in front of the same routes.
