# gorbital/guard

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/guard"
```

Package guard provides route options that decide whether a request may reach a route's handler (ADR-0082). Every route requires an authenticated actor unless it has [Public](#Public); the guards below add to that. Guards run in the order declared, group guards first, after the route's middleware and before its input is parsed, so a refused request's body is never read. Each guard documents the responses it refuses with in the OpenAPI document.

```go
books := r.Group("/v1/books", gorbital.Tags("Books"))
gorbital.Get(books, "/{id}", h.getBook, guard.Permission("books.book.read"))
gorbital.Post(books, "", h.createBook,
	guard.Permission("books.book.write"),
	guard.RateLimit(30, time.Minute))
gorbital.Get(r, "/v1/catalog/{id}", h.catalogBook, guard.Public())
```

Stability: experimental until v0.2.0 (ADR-0015, ADR-0082).

## Contents

- Constants: [`DefaultWebhookBodyLimit`](#DefaultWebhookBodyLimit)
- Functions: [`New`](#New), [`OrgMember`](#OrgMember), [`Permission`](#Permission), [`Public`](#Public), [`RateLimit`](#RateLimit), [`RecentReauth`](#RecentReauth), [`Scope`](#Scope), [`Webhook`](#Webhook)
- Types:
  - [`RateLimitOption`](#RateLimitOption): [`ByAPIKey`](#ByAPIKey), [`ByIP`](#ByIP), [`ByUser`](#ByUser), [`Named`](#Named)
  - [`Request`](#Request): [`Request.Header`](#Request.Header), [`Request.Operation`](#Request.Operation), [`Request.PathParam`](#Request.PathParam), [`Request.Query`](#Request.Query)
  - [`Spec`](#Spec)
  - [`WebhookOption`](#WebhookOption): [`WebhookBodyLimit`](#WebhookBodyLimit)

## Constants

<a id="DefaultWebhookBodyLimit"></a>

```go
const DefaultWebhookBodyLimit = 1 << 20
```

DefaultWebhookBodyLimit is the largest webhook body [Webhook](#Webhook) reads unless [WebhookBodyLimit](#WebhookBodyLimit) sets another: 1 MiB, Huma's default body limit.

*Since `v0.2.2 (unreleased)`*

## Functions

<a id="New"></a>

### func New

```go
func New(spec Spec) gorbital.RouteOption
```

New returns a custom guard. Its errors are mapped like the module's other errors, so declare them in gorbital.Module.Errors:

```go
var ErrSubscriptionRequired = errors.New("books: subscription required")

subscribed := guard.New(guard.Spec{
	Name:     "subscription",
	Statuses: []int{http.StatusPaymentRequired},
	Check: func(ctx context.Context, req guard.Request) error {
		a, _ := actor.From(ctx)
		if !plans.Active(ctx, a.ID) {
			return ErrSubscriptionRequired
		}
		return nil
	},
})
```

*Since `v0.2.2 (unreleased)`*

**Example**

```go
subscribed := guard.New(guard.Spec{
	Name:     "subscription",
	Statuses: []int{http.StatusPaymentRequired},
	Check: func(ctx context.Context, req guard.Request) error {
		a, _ := actor.From(ctx)
		if a.ID != "usr_pro" {
			return ErrSubscriptionRequired
		}
		return nil
	},
})
module := routes(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}/audio", catalogBook, subscribed)
})
module.Errors = []httpx.Mapping{{Err: ErrSubscriptionRequired, Status: http.StatusPaymentRequired, Code: "subscription_required"}}
s, err := tryMount(module)
if err != nil {
	panic(err)
}
fmt.Println(s.as("/v1/books/1/audio", &auth.Principal{UserID: "usr_pro", SessionID: "ses_1"}))
fmt.Println(s.as("/v1/books/1/audio", &auth.Principal{UserID: "usr_free", SessionID: "ses_2"}))
```

Output:

```text
200
402 subscription_required
```

<a id="OrgMember"></a>

### func OrgMember

```go
func OrgMember(permission string) gorbital.RouteOption
```

OrgMember refuses callers who aren't members of the organisation in the route's {orgId} path parameter with a role granting permission, for routes under /v1/orgs/{orgId}/ (ADR-0023, ADR-0048).

Deprecated: organisations are one scope (ADR-0088). Use [Scope](#Scope), which is this guard under the app's own tenancy; an app that mounts gorbital.dev/gorbital/orgshttp and changes nothing sees no difference. This name keeps working for all of v0.x. Its guard name in logs and metrics stays "org\_member:\<permission>".

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	invoices := r.Group("/v1/orgs/{orgId}/invoices", gorbital.Tags("Invoices"))
	gorbital.Get(invoices, "", catalogBook, guard.OrgMember("invoices.invoice.read"))
})
op := s.api.OpenAPI().Paths["/v1/orgs/{orgId}/invoices"].Get
fmt.Println(op.Extensions["x-gorbital-guards"], op.Errors)
fmt.Println(s.as("/v1/orgs/org_1/invoices", nil))

// The path must name the organisation.
_, err := tryMount(routes(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/invoices/{id}", catalogBook, guard.OrgMember("invoices.invoice.read"))
}))
fmt.Println(err)
```

Output:

```text
[authenticated org_member:invoices.invoice.read] [401 403 404 422 500]
401 unauthenticated
gorbital: module "books": GET /v1/invoices/{id}: guard.OrgMember needs the organisation ID in the path as {orgId}, such as /v1/orgs/{orgId}/invoices
```

<a id="Permission"></a>

### func Permission

```go
func Permission(name string) gorbital.RouteOption
```

Permission refuses callers without permission: 403 forbidden, or 403 mfa\_required when the caller's roles grant it only to a session signed in with a second factor. API keys hold a permission only when their scopes include it.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.Permission("books.book.read"))
})
reader := &auth.Principal{UserID: "usr_1", SessionID: "ses_1", Permissions: []string{"books.book.read"}}
stranger := &auth.Principal{UserID: "usr_2", SessionID: "ses_2"}
fmt.Println(s.as("/v1/books/bok_1", reader))
fmt.Println(s.as("/v1/books/bok_1", stranger))
```

Output:

```text
200
403 forbidden
```

<a id="Public"></a>

### func Public

```go
func Public() gorbital.RouteOption
```

Public lets requests without an authenticated actor reach the route, and removes its security requirement from the OpenAPI document. On a group, it applies to every route in the group.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r.Group("/v1/catalog", guard.Public()), "/{id}", catalogBook)
	gorbital.Get(r, "/v1/wishlist/{id}", catalogBook)
})
fmt.Println(s.as("/v1/catalog/bok_1", nil))
fmt.Println(s.as("/v1/wishlist/bok_1", nil))
```

Output:

```text
200
401 unauthenticated
```

<a id="RateLimit"></a>

### func RateLimit

```go
func RateLimit(n int, window time.Duration, opts ...RateLimitOption) gorbital.RouteOption
```

RateLimit allows n requests per window for each caller (see [ByUser](#ByUser), [ByAPIKey](#ByAPIKey), [ByIP](#ByIP)) and refuses the rest with 429 rate\_limited and a Retry-After header. Bursts of up to n requests are allowed.

With gorbital.Deps.RateLimits set, the budget is shared by every instance; without it, each instance counts on its own. A limiter that can't decide allows the request.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/exports/{id}", catalogBook, guard.RateLimit(2, time.Hour))
})
user := &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}
for range 3 {
	fmt.Println(s.as("/v1/exports/1", user))
}
```

Output:

```text
200
200
429 rate_limited
```

<a id="RecentReauth"></a>

### func RecentReauth

```go
func RecentReauth() gorbital.RouteOption
```

RecentReauth refuses a session that neither signed in nor verified a second factor within auth.RecentVerification (10 minutes), with 403 reauthentication\_required, and refuses API keys with 403 session\_required. Use it on operations that change how an account signs in or that an attacker holding a stolen session shouldn't reach.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/account/recovery-codes/{id}", catalogBook, guard.RecentReauth())
})
justSignedIn := &auth.Principal{UserID: "usr_1", SessionID: "ses_1", SignedInAt: time.Now()}
signedInYesterday := &auth.Principal{UserID: "usr_1", SessionID: "ses_2", SignedInAt: time.Now().Add(-24 * time.Hour)}
fmt.Println(s.as("/v1/account/recovery-codes/1", justSignedIn))
fmt.Println(s.as("/v1/account/recovery-codes/1", signedInYesterday))
```

Output:

```text
200
403 reauthentication_required
```

<a id="Scope"></a>

### func Scope

```go
func Scope(permission string) gorbital.RouteOption
```

Scope refuses callers who aren't members of the scope in the route's path, with a role granting permission (ADR-0088). The scope is the app's tenancy: organisations by default, or whatever the app named with gorbital.WithScope — merchants, clinics, restaurants. The path parameter is the scope's PathParam, "orgId" unless the app changed it, so routes look like /v1/orgs/{orgId}/… or /v1/merchants/{merchantId}/….

It asks the app's scope authorizer on every request:

  - 404 with the scope's refusal code ("org\_not\_found" by default) when the scope doesn't exist, is deleted, has a malformed ID, or the caller isn't a member: the four look the same, so scope IDs can't be probed;
  - 403 mfa\_required when the member's role grants permission only to a session signed in with a second factor, never to API keys;
  - 403 forbidden when the role doesn't grant it.

Members are users with a session, their API keys (within the keys' scopes), and the scope's own service accounts through their keys; a service account never reaches another scope. Platform roles grant nothing in a scope.

On success, the actor acts in the scope: its OrgID is set and its permissions are those of the member's role, so audit events carry the scope, guards after it such as [Permission](#Permission) check scope permissions, and the request's database connections carry it for row-level security (gorbital.Scope.Session, ADR-0061). Declare the permission with gorbital.Permission.ScopeRoles.

Registration fails when the path has no scope parameter or the route is public, and gorbital.New fails when the app has no scope.

*Since `v0.2.2 (unreleased)`*

**Example**

guard.Scope protects a route with the app's own tenancy: the scope ID comes from the path parameter the app's gorbital.Scope declares, and the app's scope authorizer decides who may act in it.

```go
orders := gorbital.Module{
	Name: "orders",
	Permissions: []gorbital.Permission{
		{Name: "orders.order.read", Description: "See orders", ScopeRoles: []string{"owner", "manager"}},
		{Name: "orders.order.refund", Description: "Refund an order", ScopeRoles: []string{"owner"}},
	},
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		// In an app built with gorbital.Scope{PathParam: "merchantId"}.
		g := r.Group("/v1/merchants/{merchantId}/orders", gorbital.Tags("Orders"))
		gorbital.Get(g, "/{id}", catalogBook, guard.Scope("orders.order.read"))
	},
}
for _, p := range orders.Permissions {
	fmt.Println(p.Name, p.ScopeRoles)
}
```

Output:

```text
orders.order.read [owner manager]
orders.order.refund [owner]
```

<a id="Webhook"></a>

### func Webhook

```go
func Webhook(v webhook.Verifier, opts ...WebhookOption) gorbital.RouteOption
```

Webhook refuses requests whose signature v doesn't accept, with 401 invalid\_webhook\_signature, before the route's input is parsed. It reads the raw body once, up to the body limit (413 request\_too\_large beyond it), passes it with the headers to v, and gives the same bytes to the handler. A verifier error that doesn't wrap webhook.ErrInvalidSignature, such as a key server that is down, is a 500, mapped like any guard error.

Senders have no session: combine it with [Public](#Public).

```go
payments, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{cfg.PaymentsWebhookSecret.Reveal()}})
gorbital.Post(r, "/v1/webhooks/payments", h.paymentEvent, guard.Public(), guard.Webhook(payments))
```

A verified request can still arrive twice: senders retry, and a captured request can be replayed within the verifier's tolerance. Make the handler idempotent, for example by storing the delivery ID with the change it makes.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
payments, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{webhookSecret}})
if err != nil {
	panic(err)
}
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Post(r, "/v1/webhooks/payments", paymentReceived, guard.Public(), guard.Webhook(payments))
})
body := `{"type":"payment.succeeded"}`
fmt.Println(s.deliver("/v1/webhooks/payments", signWebhook(webhookKey, "msg_1", time.Now(), body), body))
fmt.Println(s.deliver("/v1/webhooks/payments", signWebhook(webhookKey, "msg_1", time.Now(), body), `{"type":"payment.refunded"}`))
```

Output:

```text
handled payment.succeeded
204
401 invalid_webhook_signature
```

## Types

<a id="RateLimitOption"></a>

### type RateLimitOption

```go
type RateLimitOption func(*rateLimit)
```

A RateLimitOption configures [RateLimit](#RateLimit).

*Since `v0.2.2 (unreleased)`*

**Example**

```go
// Options choose what requests are counted under, and whether routes
// share a budget.
s := exampleServer(func(r *gorbital.Router) {
	imports := r.Group("/v1/imports", guard.RateLimit(1, time.Hour, guard.ByAPIKey(), guard.Named("imports")))
	gorbital.Get(imports, "/books/{id}", catalogBook)
	gorbital.Get(imports, "/shelves/{id}", catalogBook)
})
key := &auth.Principal{UserID: "usr_1", APIKeyID: "key_1"}
fmt.Println(s.as("/v1/imports/books/1", key))
fmt.Println(s.as("/v1/imports/shelves/1", key))
```

Output:

```text
200
429 rate_limited
```

<a id="ByAPIKey"></a>

#### func ByAPIKey

```go
func ByAPIKey() RateLimitOption
```

ByAPIKey counts requests per API key, so each of a user's keys has its own budget; requests with a session are counted per user.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/sync/{id}", catalogBook, guard.RateLimit(1, time.Minute, guard.ByAPIKey()))
})
laptop := &auth.Principal{UserID: "usr_1", APIKeyID: "key_laptop"}
phone := &auth.Principal{UserID: "usr_1", APIKeyID: "key_phone"}
fmt.Println(s.as("/v1/sync/1", laptop))
fmt.Println(s.as("/v1/sync/1", phone))
```

Output:

```text
200
200
```

<a id="ByIP"></a>

#### func ByIP

```go
func ByIP() RateLimitOption
```

ByIP counts requests per client address: the address after httpx.TrustedProxies, with IPv6 clients grouped by /64 (ratelimit.ClientKey).

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r.Group("/v1/catalog", guard.Public()), "/{id}", catalogBook, guard.RateLimit(1, time.Minute, guard.ByIP()))
})
fmt.Println(s.as("/v1/catalog/1", nil))
fmt.Println(s.as("/v1/catalog/1", nil))
```

Output:

```text
200
429 rate_limited
```

<a id="ByUser"></a>

#### func ByUser

```go
func ByUser() RateLimitOption
```

ByUser counts requests per authenticated user or service account, and per client address for requests without one (on a public route). It is the default.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/search/{id}", catalogBook, guard.RateLimit(1, time.Minute, guard.ByUser()))
})
ada := &auth.Principal{UserID: "usr_ada", SessionID: "ses_1"}
grace := &auth.Principal{UserID: "usr_grace", SessionID: "ses_2"}
fmt.Println(s.as("/v1/search/1", ada))
fmt.Println(s.as("/v1/search/1", ada))
fmt.Println(s.as("/v1/search/1", grace))
```

Output:

```text
200
429 rate_limited
200
```

<a id="Named"></a>

#### func Named

```go
func Named(name string) RateLimitOption
```

Named sets the limiter's name. Routes whose limits share a name share their budget, so a group can limit all its writes together; they must use the same limit. Without it, each route has its own limiter named after its operation ID. Names appear in the shared rate-limit store and in /ops/auth/rate-limits.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	writes := guard.RateLimit(1, time.Minute, guard.Named("shelf_writes"))
	gorbital.Get(r, "/v1/shelves/{id}/add", catalogBook, writes)
	gorbital.Get(r, "/v1/shelves/{id}/remove", catalogBook, writes)
})
user := &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}
fmt.Println(s.as("/v1/shelves/1/add", user))
fmt.Println(s.as("/v1/shelves/1/remove", user))
```

Output:

```text
200
429 rate_limited
```

<a id="Request"></a>

### type Request

```go
type Request struct {
	// contains filtered or unexported fields
}
```

A Request is what a custom guard can read about the request before its input is parsed.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.New(guard.Spec{Name: "print", Check: printRequest}))
})
s.as("/v1/books/bok_1?format=epub", &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}, "Accept-Language", "en")
```

Output:

```text
books-get-v1-books-by-id bok_1 epub en
```

<a id="Request.Header"></a>

#### func (Request) Header

```go
func (r Request) Header(name string) string
```

Header returns the first value of a request header.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.New(guard.Spec{Name: "client_version", Check: func(_ context.Context, req guard.Request) error {
		if req.Header("X-App-Version") < "2.4.0" {
			return httpx.NewProblem(http.StatusUpgradeRequired, "app_outdated", "update the app to continue")
		}
		return nil
	}}))
})
user := &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}
fmt.Println(s.as("/v1/books/1", user, "X-App-Version", "2.3.0"))
fmt.Println(s.as("/v1/books/1", user, "X-App-Version", "2.4.1"))
```

Output:

```text
426 app_outdated
200
```

<a id="Request.Operation"></a>

#### func (Request) Operation

```go
func (r Request) Operation() *huma.Operation
```

Operation returns the route's OpenAPI operation, such as its ID and path.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", catalogBook, gorbital.OperationID("books-get"), guard.New(guard.Spec{Name: "print", Check: func(_ context.Context, req guard.Request) error {
		fmt.Println(req.Operation().OperationID, req.Operation().Path)
		return nil
	}}))
})
s.as("/v1/books/1", &auth.Principal{UserID: "usr_1", SessionID: "ses_1"})
```

Output:

```text
books-get /v1/books/{id}
```

<a id="Request.PathParam"></a>

#### func (Request) PathParam

```go
func (r Request) PathParam(name string) string
```

PathParam returns the value of a path parameter, such as "id" in /v1/books/{id}.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.New(guard.Spec{Name: "print", Check: func(_ context.Context, req guard.Request) error {
		fmt.Println(req.PathParam("id"))
		return nil
	}}))
})
s.as("/v1/books/bok_42", &auth.Principal{UserID: "usr_1", SessionID: "ses_1"})
```

Output:

```text
bok_42
```

<a id="Request.Query"></a>

#### func (Request) Query

```go
func (r Request) Query(name string) string
```

Query returns the first value of a query parameter.

*Since `v0.2.2 (unreleased)`*

**Example**

```go
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", catalogBook, guard.New(guard.Spec{Name: "print", Check: func(_ context.Context, req guard.Request) error {
		fmt.Printf("%q\n", req.Query("preview"))
		return nil
	}}))
})
s.as("/v1/books/bok_1?preview=true", &auth.Principal{UserID: "usr_1", SessionID: "ses_1"})
```

Output:

```text
"true"
```

<a id="Spec"></a>
<a id="Spec.Name"></a>
<a id="Spec.Statuses"></a>
<a id="Spec.Check"></a>

### type Spec

```go
type Spec struct {
	// Name identifies the guard in errors, metrics and the OpenAPI
	// document. It is lowercase snake_case, such as "subscription".
	Name string
	// Statuses are the error statuses Check's errors map to, for the
	// OpenAPI document.
	Statuses []int
	// Check returns nil to allow the request. To refuse it, return an error
	// mapped by the module's Errors (or an *httpx.Problem); any other error
	// is a 500, logged once with the request ID.
	Check func(ctx context.Context, req Request) error
}
```

A Spec describes a custom guard for [New](#New).

*Since `v0.2.2 (unreleased)`*

**Example**

```go
// A guard that checks a path parameter against the caller.
ownShelf := guard.Spec{
	Name:     "own_shelf",
	Statuses: []int{http.StatusNotFound},
	Check: func(ctx context.Context, req guard.Request) error {
		a, _ := actor.From(ctx)
		if req.PathParam("owner") != a.ID {
			return httpx.NewProblem(http.StatusNotFound, "shelf_not_found", "no shelf of yours has this ID")
		}
		return nil
	},
}
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/users/{owner}/shelves/{id}", catalogBook, guard.New(ownShelf))
})
me := &auth.Principal{UserID: "usr_1", SessionID: "ses_1"}
fmt.Println(s.as("/v1/users/usr_1/shelves/1", me))
fmt.Println(s.as("/v1/users/usr_2/shelves/1", me))
```

Output:

```text
200
404 shelf_not_found
```

<a id="WebhookOption"></a>

### type WebhookOption

```go
type WebhookOption func(*webhookGuard)
```

A WebhookOption configures [Webhook](#Webhook).

*Since `v0.2.2 (unreleased)`*

**Example**

```go
// Options follow the verifier.
github, err := webhook.NewHMAC(webhook.HMACConfig{
	Secrets:         [][]byte{[]byte("It's a Secret to Everybody")}, // gitleaks:allow (GitHub's published test vector)
	SignatureHeader: "X-Hub-Signature-256",
	SignaturePrefix: "sha256=",
	Encoding:        webhook.Hex,
})
if err != nil {
	panic(err)
}
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Post(r, "/v1/webhooks/github", paymentReceived, guard.Public(), guard.Webhook(github, guard.WebhookBodyLimit(25<<20)))
})
header := http.Header{"X-Hub-Signature-256": {"sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"}}
// The signature is for "Hello, World!", not this body.
fmt.Println(s.deliver("/v1/webhooks/github", header, `{"type":"push"}`))
```

Output:

```text
401 invalid_webhook_signature
```

<a id="WebhookBodyLimit"></a>

#### func WebhookBodyLimit

```go
func WebhookBodyLimit(n int64) WebhookOption
```

WebhookBodyLimit sets the largest body [Webhook](#Webhook) reads, in bytes. Larger requests are refused with 413 request\_too\_large before they are verified. Default: [DefaultWebhookBodyLimit](#DefaultWebhookBodyLimit).

*Since `v0.2.2 (unreleased)`*

**Example**

```go
payments, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{webhookSecret}})
if err != nil {
	panic(err)
}
s := exampleServer(func(r *gorbital.Router) {
	gorbital.Post(r, "/v1/webhooks/payments", paymentReceived, guard.Public(), guard.Webhook(payments, guard.WebhookBodyLimit(16)))
})
body := `{"type":"payment.succeeded"}`
fmt.Println(s.deliver("/v1/webhooks/payments", signWebhook(webhookKey, "msg_1", time.Now(), body), body))
```

Output:

```text
413 request_too_large
```
