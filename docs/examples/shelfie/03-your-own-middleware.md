# 3. Your own middleware and guards

Two of Shelfie's rules are nobody's business but Shelfie's: which versions of the mobile app the books routes still answer, and who has paid for Shelfie Plus. The first is middleware, the second is a guard, and this chapter writes both by hand in the books module. [Chapter 9](09-generators.md) generates the same two files with `orb gen middleware`.

## 1. Which of the two to write

A request reaches a books route in this order ([Guards and middleware](../../guides/guards-and-middleware.md#the-order-a-request-goes-through)):

```text
the app's stack → the module's middleware → group Use → route Use
  → the sign-in check → guards → input parsing → the handler
```

| | Middleware | Guard |
|---|---|---|
| Is | `func(http.Handler) http.Handler`, plain `net/http` | `guard.New(guard.Spec{…})`, a check returning an error |
| Runs | Before the sign-in check, so for anonymous callers too | After it, so the caller is known |
| Can | Refuse, add context values, read the response afterwards | Refuse |
| Shows up in the OpenAPI document | Only what you declare with `gorbital.Errors` | Its statuses and its name in `x-gorbital-guards` |

A version header is on the request whether or not anyone is signed in, so it's middleware. A subscription belongs to a reader, so it's a guard.

## 2. Middleware: refusing old apps

Shelfie's mobile app sends `X-App-Version`. Builds before 2.0.0 send book statuses the module no longer accepts, and the kindest thing to do with one is to stop it at the door:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/require_client_version.go#require-client-version -->

Four things worth copying:

- **Errors go through `httpx.WriteProblem`**, never `http.Error`, so a refusal from middleware has the same JSON shape, `code` and request ID as one from a guard or a handler.
- **A refused request returns without calling `next`.** Call it at most once, and don't write to `w` afterwards.
- **The rule is its own function**, so it can be tested without a server, and read without reading the plumbing around it.
- **A missing header is not a refusal.** The web app and `curl` send no version; only the mobile app does.

`routes.go` puts it in front of every books route, and declares the status it answers with, because middleware — unlike a guard — adds nothing to the OpenAPI document by itself:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/routes.go#books-group -->

<!-- include examples/apps/shelfie/internal/modules/books/client_version_test.go#client-version-refusals -->

Because middleware runs before the sign-in check, an old app with no session is told to update rather than to sign in. That is the right order: signing in wouldn't have helped it.

## 3. Getting the value to the handler

The middleware has already parsed the version; parsing it again in a handler would be a second chance to disagree. It travels in the request's context, under a key of a type the module declares, so nothing outside the package can read the value or overwrite it — never a bare string:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/require_client_version.go#client-version -->

The context reaches Huma handlers unchanged, so a handler reads it back with one call:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/export_books.go#export-books -->

<!-- include examples/apps/shelfie/internal/modules/books/client_version_test.go#client-version-in-context -->

## 4. Reading the response with `httpx.Capture`

When the mobile team asks which build is failing, the access log has the status and not the version, and the module's middleware has the version and not the status: by the time `next` returns, the handler has written the response and `w` won't say what it was. [`httpx.Capture`](../../methods/httpx.md#Capture) is the missing half — a response writer that records the status and size the handler wrote:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/require_client_version.go#capture -->

`httpx.AccessLog` has already wrapped `w` in the app's stack, and `Capture` returns that same record rather than a second wrapper, so the status read here is exactly the one the log line will carry. `AccessNote` puts the version on that one line, and only on the lines somebody will read:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/require_client_version_test.go#capture-test -->

## 5. A guard: an active subscription

Exporting a whole shelf is what Shelfie Plus is for. The state is one row per reader, active while its end date is in the future, so a lapse needs no nightly job:

<!-- include examples/apps/shelfie/db/migrations/20260920000007_subscriptions.sql#subscriptions-table -->

The question the guard asks is a use case, with its own small port, kept out of the books `Store` because it isn't about books; when Shelfie grows a billing module, this port is what moves behind it:

<!-- include examples/apps/shelfie/internal/modules/books/usecase/subscriptions.go#subscriptions -->

The guard itself is the check, its name, and the error it refuses with:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/active_subscription.go#active-subscription -->

`module.go` maps that error like any other error of the module, which is what turns it into a status and a stable code:

<!-- include examples/apps/shelfie/internal/modules/books/module.go#subscription-error -->

and `routes.go` uses it next to the permission, because they are different questions — every reader holds `books.book.read`, and not every reader pays:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/routes.go#export-route -->

<!-- include examples/apps/shelfie/internal/modules/books/subscription_test.go#subscription-guard -->

The guard runs before the body is read and after the sign-in check, so it can ask `actor.From` who is calling, and a reader who isn't subscribed never reaches the query that would have read their shelf. Its refusal joins the others in the route's documentation: `x-gorbital-guards` on `GET /v1/books/export` is `["authenticated", "permission:books.book.read", "active_subscription"]`. Anything the check returns that *isn't* a mapped error is a 500 with a generic detail, logged once: a refusal never leaks why.

Both files are laid out the way `orb gen middleware` writes them — the rule in a `check…` function of its own, the guard's error named after the guard, a table-driven test beside it — so a generated skeleton and a hand-written rule read the same a year later ([chapter 9](09-generators.md)).

## Next

[4. Tests](04-tests.md): the books module, its guards and this chapter's rules, tested through the whole app.
