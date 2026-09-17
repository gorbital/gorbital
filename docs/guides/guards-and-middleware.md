# Guards and middleware

**Guards** decide whether a request may reach a route: sign-in, permissions, rate limits, re-authentication, or your own rule. **Middleware** wraps a route to add behaviour: context values, headers, logging, timing. Both are route options next to the route they protect ([Modules and routes](modules-and-routes.md)). Packages: `gorbital.dev/gorbital/guard` ([Methods](../methods/gorbital-guard.md)) and `gorbital.Use` ([Methods](../methods/gorbital.md#Use)); decision: [ADR-0082](../adr/0082-routes-guards-and-middleware.md).

```go
books := r.Group("/v1/books", gorbital.Tags("Books"), gorbital.Use(requireClientVersion("2.4.0")))
gorbital.Get(books, "/{id}", h.getBook, guard.Permission(PermRead))
gorbital.Post(books, "", h.createBook,
	guard.Permission(PermWrite),
	guard.RateLimit(30, time.Minute))
gorbital.Get(r.Group("/v1/catalog", guard.Public()), "/{id}", h.catalogBook)
```

## The order a request goes through

```text
request
  → the app's middleware stack (recovery, request ID, CORS, authentication, …)
  → the module's Middleware
  → group Use, outer groups first
  → route Use
  → the sign-in check (skipped with guard.Public)
  → guards, group guards first, in the order written
  → input parsing and validation (Huma)
  → your handler
```

Everything before input parsing runs **before the body is read**: an unauthenticated or unauthorized request never costs a JSON decode, and never learns what validation would have said.

Middleware runs **before** the sign-in check, so a module can bring its own authentication as middleware (see [Middleware that sets the actor](#middleware-that-sets-the-actor)). The flip side: middleware also runs for anonymous callers, so keep expensive work in guards or the handler.

## Built-in guards

| Guard | Allows | Refuses with |
|---|---|---|
| *(none)* | Any authenticated user, service account or API key | 401 `unauthenticated` |
| `guard.Public()` | Anyone, and removes the security requirement from the docs | — |
| `guard.Permission("books.book.write")` | Callers holding the permission; API keys only within their scopes | 403 `forbidden`; 403 `mfa_required` when the caller's role grants it only to a session signed in with a second factor |
| `guard.RecentReauth()` | A session that signed in or verified a second factor in the last 10 minutes | 403 `reauthentication_required`; 403 `session_required` for API keys |
| `guard.RateLimit(n, window, …)` | n requests per window per caller, with bursts up to n | 429 `rate_limited` with `Retry-After` |
| `guard.Webhook(verifier, …)` | Requests signed by a webhook sender, checked on the raw body | 401 `invalid_webhook_signature`; 413 `request_too_large` above the body limit |
| `guard.OrgMember("invoices.invoice.read")` | On a route under `/v1/orgs/{orgId}/`, members of that organisation whose role grants the permission, their API keys within scopes, and the organisation's service accounts; needs [`orgshttp`](../start/organisations.md#in-an-app-on-gorbitalmain) | 404 `org_not_found` for any organisation the caller isn't in; 403 `forbidden`; 403 `mfa_required`. On success the actor acts in the organisation, for audit events and [row-level security](row-level-security.md#in-an-app-on-gorbitalmain) |
| `guard.New(guard.Spec{…})` | Whatever your check says | The error your check returns |

Each guard adds its responses to the route's OpenAPI operation, and lists itself in `x-gorbital-guards` (`["authenticated", "permission:books.book.write", "rate_limit:30/1m0s"]`), which the docs and the Dev Portal show.

`guard.Public()` on a group applies to every route in it. There is no way to remove a guard or middleware a group added from one of its routes: put routes that need something different in their own group.

### Permissions

`guard.Permission` checks the permission the actor holds, which the authentication middleware computed from the caller's roles ([permissions reference](../reference/permissions.md)). Declare the permission on the module so it exists in the catalog:

```go
Permissions: []gorbital.Permission{{Name: PermWrite, Description: "Add, change and remove your books", Roles: []string{"user"}}},
```

A permission is not ownership: `books.book.write` lets a user write *books*, not *other people's* books. Check ownership in the handler or the store (`WHERE owner_id = $1`), or with a [custom guard](#writing-a-guard).

### Rate limits

```go
guard.RateLimit(30, time.Minute)                                        // per user (default)
guard.RateLimit(1000, time.Hour, guard.ByAPIKey())                      // per API key
guard.RateLimit(10, time.Minute, guard.ByIP())                          // per client address
r.Group("/v1/shelves", guard.RateLimit(60, time.Minute, guard.Named("shelf_writes"))) // one budget for the group
```

| Option | Counts requests per | Use for |
|---|---|---|
| `ByUser()` (default) | User or service account; client address when there is none (public routes) | Most routes |
| `ByAPIKey()` | API key, so each of a user's keys has its own budget; sessions per user | Public APIs sold per key |
| `ByIP()` | Client address after `httpx.TrustedProxies`, IPv6 grouped by /64 | Public routes, sign-up forms |
| `Named("name")` | — shares one budget between every route with that name; they must use the same limit | A group's writes together |

- **Shared across instances** when `gorbital.Deps.RateLimits` is set (`ratelimitpg`, [ADR-0052](../adr/0052-shared-rate-limits.md)); otherwise each instance counts on its own. When the database can't answer, `ratelimitpg` decides in memory, and a limiter that can't decide at all allows the request: rate limits slow abuse, they don't lock out users.
- **Behind a load balancer**, set `APP_TRUSTED_PROXIES`, or every client shares the balancer's address for `ByIP` and anonymous `ByUser` keys.
- A limiter without `Named` is named after the route's operation ID.

### Webhooks

`guard.Webhook` verifies a provider's signature on the raw body before Huma parses it, then hands the handler the same bytes. Senders have no session, so combine it with `guard.Public()`:

```go
payments, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{secret}})
// …
gorbital.Post(r, "/v1/webhooks/payments", h.paymentEvent,
	guard.Public(),
	guard.Webhook(payments, guard.WebhookBodyLimit(256<<10)))
```

| Option or verifier | What it does |
|---|---|
| `guard.WebhookBodyLimit(n)` | Largest body read, in bytes (default 1 MiB); larger requests get 413 before verification |
| `webhook.NewStandard` | Standard Webhooks and Svix (`HeaderPrefix: "svix-"`, for Resend and Clerk): ID, timestamp within 5 minutes, several secrets for rotation |
| `webhook.NewHMAC` | Other HMAC-SHA256 senders (GitHub, Shopify, Slack) |
| Your own `webhook.Verifier` | Any other scheme; return an error wrapping `webhook.ErrInvalidSignature` to refuse |

A verified delivery can still arrive twice (retries, replays within the window): make the handler idempotent. Details, sender settings and a custom verifier: [Security layers](security-layers.md#signed-webhooks).

### Re-authentication

`guard.RecentReauth()` protects operations a stolen session shouldn't reach: changing sign-in methods, showing recovery codes, deleting the account. Clients that get `reauthentication_required` ask the person to sign in again (or verify their second factor) and retry.

## Writing a guard

A guard is a check: it reads the request and returns `nil` to allow it, or an error to refuse it.

```go
var ErrSubscriptionRequired = errors.New("books: subscription required")

func requireSubscription(plans *Plans) gorbital.RouteOption {
	return guard.New(guard.Spec{
		Name:     "subscription",
		Statuses: []int{http.StatusPaymentRequired},
		Check: func(ctx context.Context, req guard.Request) error {
			a, _ := actor.From(ctx) // the sign-in check has run
			active, err := plans.Active(ctx, a.ID)
			if err != nil {
				return err // a 500, logged once with the request ID
			}
			if !active {
				return ErrSubscriptionRequired // mapped below: 402 subscription_required
			}
			return nil
		},
	})
}
```

Map its errors with the module's other errors, and use it like any guard:

```go
Errors: []httpx.Mapping{
	{Err: ErrSubscriptionRequired, Status: http.StatusPaymentRequired, Code: "subscription_required", Detail: "this needs an active subscription"},
},
Routes: func(r *gorbital.Router, d gorbital.Deps) {
	premium := r.Group("/v1/books/{id}/audio", requireSubscription(plans))
	gorbital.Get(premium, "", h.streamAudio)
},
```

| `guard.Spec` field | What it is |
|---|---|
| `Name` | Lowercase snake_case; appears in metrics, traces and `x-gorbital-guards` |
| `Statuses` | The statuses your errors map to, for the docs |
| `Check` | `func(ctx context.Context, req guard.Request) error` |

`guard.Request` gives you what exists before parsing: `PathParam("id")`, `Query("format")`, `Header("X-App-Version")` and `Operation()` (ID, path, tags). The body isn't available: a check that needs it belongs in the handler.

A check can also return an `*httpx.Problem` directly (`httpx.NewProblem(http.StatusNotFound, "shelf_not_found", "…")`) when the refusal is specific to the guard. Any other error is a 500 with a generic detail, logged once: never put internal details in a refusal.

## Writing middleware

Middleware is plain `net/http`: `func(http.Handler) http.Handler`, the same type as `httpx.Middleware`. Anything that works with Go's standard library works here. `orb gen middleware <Name> --module <module>` (or `--global`, or `--guard` for a guard) writes the skeleton and a table-driven test ([Generating code](generating-code.md#middleware-and-guards)).

```go
// requireClientVersion refuses mobile apps older than min.
func requireClientVersion(min string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-App-Version") < min {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUpgradeRequired, "app_outdated", "update the app to continue"))
				return // don't call next
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

Attach it at the level it belongs to:

| Level | How |
|---|---|
| Every route of a module | `gorbital.Module{Middleware: []func(http.Handler) http.Handler{…}}` |
| A group | `r.Group("/v1/books", gorbital.Use(mw))` |
| One route | `gorbital.Get(r, "/v1/books/{id}", h.getBook, gorbital.Use(mw))` |
| The whole app | The app's stack: `routes.go` in a v0.1 app; `gorbital.WithMiddleware` in an app on `gorbital.Main` ([The middleware stack](middleware-stack.md)) |

### Rules

1. **Call `next.ServeHTTP` at most once, and don't write to `w` after it.** To stop a request, write a response and return.
2. **Write errors with `httpx.WriteProblem`**, never `http.Error`, so every error has the same JSON shape, code and request ID.
3. **Pass values with `r.WithContext`**, under an unexported key, and read them back with a `From` function, as `actor.From` and `requestid.From` do.
4. **Read the response with `httpx.Capture`**, which records the status and size the handler wrote:

   ```go
   cw := httpx.Capture(w)
   next.ServeHTTP(cw, r)
   if cw.Status() >= 500 { … }
   ```

5. **Pass `r.Context()` to anything that waits** (queries, HTTP calls), so a cancelled request stops them.
6. **Never log tokens, passwords, email addresses or secrets.** Log IDs ([ADR-0007](../adr/0007-observability.md)).
7. **Build what's expensive in the constructor**, not per request: route middleware is built once per route.

### Passing a value to the handler

```go
type clientKey struct{}

type Client struct{ Platform, Version string }

func clientInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := Client{Platform: r.Header.Get("X-Platform"), Version: r.Header.Get("X-App-Version")}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientKey{}, c)))
	})
}

// ClientFrom returns the client clientInfo stored.
func ClientFrom(ctx context.Context) (Client, bool) {
	c, ok := ctx.Value(clientKey{}).(Client)
	return c, ok
}
```

The handler reads it with `ClientFrom(ctx)`: the context reaches Huma handlers unchanged.

### Middleware that sets the actor

Because middleware runs before the sign-in check, a module can authenticate callers its own way, such as a partner's signed token, by setting the actor:

```go
func partnerToken(verify func(token string) (string, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id, ok := verify(r.Header.Get("X-Partner-Token")); ok {
				r = r.WithContext(actor.With(r.Context(), actor.Actor{Kind: actor.KindService, ID: id}))
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

A request without a valid token has no actor, so the route answers 401 as usual.

For tokens from an external identity provider (Auth0, Clerk, Supabase, Firebase, Cognito), use `gorbital.dev/modules/jwt` rather than writing this yourself: it handles key rotation, algorithms, audiences and clock skew ([Security layers](security-layers.md#external-identity-providers-jwt)).

## Testing

Mount the module on a test API ([Modules and routes](modules-and-routes.md#testing-a-module)) and set the caller the way the authentication middleware does, with `auth.WithPrincipal`:

```go
req := httptest.NewRequest(http.MethodPost, "/v1/books", strings.NewReader(`{"title":"Dune"}`))
req.Header.Set("Content-Type", "application/json")
req = req.WithContext(auth.WithPrincipal(req.Context(), auth.Principal{
	UserID: "usr_1", SessionID: "ses_1", Permissions: []string{"books.book.write"},
}))
```

Test a middleware on its own with `httptest`: wrap a handler, send a request, check the response.

## Observability

- Every refusal increments `gorbital.guard.refusals`, with the guard's name, the module and the route pattern. A spike of `permission:…` refusals after a deploy usually means a role lost a permission; `rate_limit:…` refusals show who is hitting limits.
- The request's span gets `gorbital.guard.refused` set to the guard's name.
- Refusals aren't logged one by one: the access log line has the status.

## Performance

Measured with `BenchmarkRouteMiddleware` in `gorbital/bench_test.go` (Apple M1 Max, Go 1.26.0, `-count 3`), for a signed-in GET:

| Route | Time | Memory | Allocations |
|---|---|---|---|
| No route middleware or guards | 1.35 µs | 1690 B | 21 |
| `guard.Permission` | 1.36–1.39 µs | 1690 B | 21 |
| `gorbital.Use` with 1 middleware | 1.49–1.54 µs | 2130 B | 26 |
| `gorbital.Use` with 5 middlewares | 1.53–1.56 µs | 2130 B | 26 |

Guards add no allocations. Route middleware costs a fixed 5 allocations per request (the request's context copy and Huma's context around the chain), whatever the number of middlewares, because the chain is built once per route. Middleware that every route needs is cheaper in the app's stack.
