# Plateful

A restaurant delivery platform, built on gorbital. One platform, many
restaurants, and **three kinds of caller** — which is the reason this app
exists as an example rather than another single-tenant CRUD API.

| Who | What they are | How a route lets them in |
|---|---|---|
| **Platform staff** | Accounts with the `platform_admin` or `ops_viewer` role | `/ops/*`, and `/v1/platform/*` with `guard.Permission` |
| **Restaurant staff** | Members of a restaurant's organisation, with the role `owner`, `admin` or `member` | `/v1/orgs/{orgId}/*` with `guard.OrgMember` |
| **Customers and couriers** | Ordinary signed-in accounts that belong to **no organisation at all** | `/v1/*` with `guard.Permission` on a permission the `user` role holds, and an ownership check in the use case |

The third row is the interesting one, and most of this app is about it. A
customer orders from a restaurant they are not a member of; a courier
delivers for many restaurants and is a member of none. `guard.OrgMember` is
the wrong tool for both, and there is no role that means "the customer of
this order". See
[`internal/modules/orders/usecase/get_order.go`](internal/modules/orders/usecase/get_order.go)
for the three shapes side by side, and the **Limits** section below for where
that leaves you.

## Running it

```bash
orb dev                    # the database, the migrations, the API and the Dev Portal
```

Or without the CLI:

```bash
cp .env.example .env       # then set AUTH_ENCRYPTION_KEYS: echo "k1:$(openssl rand -base64 32)"
docker compose up -d --wait
set -a; . ./.env; set +a
go run ./cmd/api migrate && go run ./cmd/api seed && go run ./cmd/api
```

Tests need PostgreSQL, as the library's do:

```bash
GORBITAL_TEST_DATABASE_URL='postgres://gorbital:gorbital@127.0.0.1:5432/plateful?sslmode=disable' \
  go test -race ./...
```

## The modules

| Module | What it is | Worth reading for |
|---|---|---|
| [`restaurants`](internal/modules/restaurants) | The tenant's own profile, and its status | One table three kinds of caller reach; a platform-only operation |
| [`menus`](internal/modules/menus) | Sections, dishes, prices, stock | Money as integer minor units; a customer-facing read of a tenant's data |
| [`orders`](internal/modules/orders) | The heart: a state machine from `placed` to `delivered` | Three ownership shapes, one transaction over four tables, a custom guard, module middleware, a job, a read model |
| [`couriers`](internal/modules/couriers) | A courier's profile and availability | The one table with **no `org_id`** |
| [`payments`](internal/modules/payments) | A payment per order, confirmed by webhook | A rule that spans two modules; idempotency; `guard.Webhook` |
| [`reviews`](internal/modules/reviews) | A rating for a delivered order | A third ownership shape, and the app's only **public** route |
| [`images`](internal/modules/images) | Menu photos and restaurant covers | `modules/storage`, and what a signed URL can and cannot do |
| [`notifications`](internal/modules/notifications) | Outbound webhooks to a restaurant's chat | **Extending the framework with something it does not have** |

### Why `notifications` is a module and not a package inside `orders`

gorbital has no outbound webhooks and no notification channel but email.
Plateful adds one, and it is a module of its own for three reasons. It owns a
table (`notification_endpoints`) and the routes that manage it, which a
package inside another module could not register cleanly. It has jobs of its
own, which `Module.Jobs` declares. And two different modules ask it for
something — `orders` when an order is placed and when one runs late — so it
cannot live inside either of them: a module never imports another module's
layers. What the others use is its **root package**
([`notify.go`](internal/modules/notifications/notify.go)), which the
architecture test explicitly allows.

It is also the module `main.go` adds by hand rather than `modules.All()`,
because `Module(opts ...Option)` takes configuration —
`orb gen modules` lists only a module whose `Module()` takes none.

### How the modules talk to each other

They don't, in Go. `internal/modules/architecture_test.go` forbids a module
from importing another module's layers, and the only exception is another
module's root package. So a rule that spans modules is written where the
transaction is, in SQL, against a table the other module owns, with a comment
at both ends saying so:

- `orders` reads `order_payments.status` before it lets a restaurant accept
  ([`repository/store.go`](internal/modules/orders/repository/store.go),
  `payment-status-sql`);
- `orders` reads `menu_items` and decrements stock in the order's transaction;
- `orders` reads and writes `couriers.active_order_id` when a courier is
  assigned;
- `reviews` reads `orders` to check an order was delivered to this customer.

That works, and it is the honest shape. It is also the framework's sharpest
edge: see **Limits**.

## The seven things a tutorial usually can't show

1. **A non-CRUD operation with real rules** — `POST .../orders/{id}/accept`
   and `/reject`. The state machine is `domain.Order.MoveTo`; the payment
   rule is `usecase.AcceptOrder`; the handler is four lines.
2. **One transaction over several tables** — `usecase.PlaceOrder` writes the
   order, its lines and the stock the dishes take, and enqueues the
   restaurant's notification with `jobs.Client.InsertTx` **in the same
   transaction**, so a rollback takes the job with it.
3. **A read model that is real SQL** —
   `repository/select_daily_summary.go`: counts by status, revenue, average
   preparation time and the busiest hour, in one query with `FILTER` clauses
   and a grouped sub-query.
4. **Listing done properly** — cursor pagination with `gorbital.dev/page`,
   filters and sorts. The owner filter is in **every** query and never in
   the cursor, because a cursor is opaque but not signed.
5. **A custom guard** — `guard.New` refusing an order at a restaurant that
   isn't open (`delivery/guards.go`). It is why placing an order names the
   restaurant in the **path**: `guard.Request` can never see the body.
6. **Module middleware** — `gorbital.Use` on the customer-facing group, so a
   runtime setting can pause ordering platform-wide
   (`delivery/middleware.go`), plus `gorbital.Timeout` on the one expensive
   route.
7. **A use case with no route** — `usecase.LateOrders`, which only the
   `orders_late_sweep` job calls, and which a test calls directly.

## Runtime settings and feature flags

| Key | Kind | What it changes |
|---|---|---|
| `orders.max_open_per_restaurant` | setting | When a kitchen stops taking orders (409 `restaurant_busy`) |
| `orders.late_after` | setting | How long before the sweep calls an order late |
| `orders.ordering_paused` | setting | A platform-wide stop on customer writes (503 `ordering_paused`) |
| `restaurants.max_delivery_radius_m` | setting | The largest radius a restaurant may claim |
| `images.max_bytes` | setting | The largest image an upload may confirm |
| `orders.scheduled_ordering` | flag, `flags.Client()` | The customer app reads it from `GET /v1/flags` and shows the "order for later" control; the API refuses `scheduled_for` while it is off |
| `orders.courier_auto_assign` | flag, server-side | Gives a ready order to a free courier automatically |

`orders.courier_auto_assign` is the one to read for **targeting**. It is read
with `Evaluate(ctx)` rather than `Enabled(ctx)` so the reason is available
when nothing is assigned, and it is rolled out by organisation: the bucket
subject is the organisation when the caller acts in one and the actor
otherwise, so a percentage moves whole restaurants at a time — and an
anonymous caller, having neither, only ever sees 0% or 100%.
`TestCourierAutoAssignRollout` allows one restaurant explicitly and leaves
another out.

## File storage

Menu photos and restaurant covers go through `gorbital.dev/modules/storage`,
in the only shape its primitives allow:

1. `POST /v1/orgs/{orgId}/images` records a pending row and returns a **signed
   PUT** URL, valid for fifteen minutes;
2. the client PUTs the bytes straight to storage;
3. `POST .../images/{id}/confirm` calls `Stat` and records the real size and
   content type, refusing anything too large or of the wrong type;
4. `GET .../images/{id}` returns a **signed GET** URL, valid for five minutes.

The object key is `orgs/{orgID}/images/{imageID}.{ext}` — derived from the
organisation, so one restaurant's key can never name another's object — and
every read re-checks `org_id` on the row before signing anything.

In development the `local` driver works with no configuration and gorbital
serves its signed URLs on the app itself at `/storage/...`; in production
`STORAGE_DRIVER=s3` and `cmd/api/storage.go` build an S3 client, because
gorbital does not import one. Note `APP_MAX_BODY_BYTES` in `.env.example`:
in development the upload travels through the app, so the app's body limit
bounds it too.

## Limits: where the framework fought us

This app was built partly to find these. They are not bugs in the code below;
they are the places a reader will get stuck.

**1. There is no primitive between "signed in" and "member of this
organisation".** `guard.OrgMember(perm)` is declarative, named, visible in
the OpenAPI document and impossible to forget. For a customer or a courier
the equivalent does not exist: the route carries
`guard.Permission("orders.order.place")`, a permission the `user` role
grants to *every* account, so it authorises a class of caller and nothing
about a row. The real rule — `order.CustomerID != caller` — is ordinary Go
inside the use case, invisible to the route table, to `api/surface.json` and
to any audit of guards. Every customer-facing route in this app is one
forgotten comparison away from an IDOR, and nothing but a test would notice.

**2. A cross-tenant route looks exactly like an isolating one.**
`guard.OrgMember(perm)` on `/v1/orgs/{orgId}/available-couriers` is
byte-for-byte what it is on every tenant-scoped route, but the `orgId` proves
standing, not scope, and plays no part in the query. Nothing distinguishes
the two, and that is the mistake that leaks data.

**3. A rule that spans two modules can only be a column name.** "A
restaurant may not accept an order until its payment is authorised" now lives
in two places that cannot see each other: `order_payments.status`, written by
the payments module, and a SQL string in the orders module. The layering rule
forbids the import that would make it one thing, and there is no sanctioned
read port. Rename the column and every test in both modules still passes.
Since the architecture test *already* allows importing another module's root
package, a blessed `func PaymentStatus(ctx, db, orderID)` there would make
the rule compile-checked for nothing.

**4. There is no "an organisation was created" hook.** `orgshttp` gives a new
account its personal workspace and creates organisations on request without
telling the app's modules, so a restaurant profile cannot appear when the
restaurant does. It appears the first time its owner saves one, with
`version: 0`. `authhttp` has `OnRegister`, but it runs *inside* the
account's transaction and the personal workspace is created *after* it
commits — so an `OnRegister` hook cannot touch the new account's
organisation either.

**5. `Deps.Jobs` is nil inside `Module.Jobs`.** Documented, and still the
awkwardest thing here. A worker that enqueues cannot be handed a client when
it is built, so it takes one from the context with
`river.ClientFromContextSafely` when it runs — a runtime failure mode where a
compile-time guarantee should be. The same is why the service the sweep keeps
has a nil transaction manager.

**6. Audit events from a worker lose their organisation.** `audit.FromContext`
fills `OrgID` from the actor, and a worker has none, so delivery and sweep
events would land with an empty `org_id` and be invisible to every org-scoped
audit view. Every such event here sets `ActorKind`, `ActorID` and `OrgID` by
hand. The failure is silent.

**7. Outbound HTTP is entirely yours.** The SSRF dial-time check, the
redirect policy, the timeout ladder, the bounded body read and the
status-code classification in
[`notifications/sender.go`](internal/modules/notifications/sender.go) are
code a framework with outbound webhooks would own once and correctly. Worse,
`*url.Error` quotes the whole URL, so the most natural line of Go —
`fmt.Errorf("send: %w", err)` — leaks a live webhook secret into the job's
error history. The defence here is a `domain.URL` whose `String()` is the
redacted form and one named `Secret()` with two callers.

**8. A signed URL carries no conditions.** `SignedURL(ctx, key, method,
expiry)` is the whole surface: GET and PUT only, no signed POST policy, no
`Content-Length` or `Content-Type` binding, no multipart helper. So
`images.max_bytes` cannot be enforced where it matters — an oversized file
uploads successfully and is refused afterwards, with the bytes already paid
for — and a confirmed content type is a claim the uploader made, not a fact
about the file.

**9. Smaller ones.** Huma refuses a pointer in a query parameter, so an
optional time is a string you parse yourself; it reads the query tags of a
struct embedded *directly* in the input and silently ignores a second level
of embedding, which is how a `limit` that does nothing gets written. A type
name is global to the app's OpenAPI schemas, so two packages called
`delivery` cannot both have a `CourierResponse`. `Module.RateLimiters` and
`guard.RateLimit(guard.Named(...))` refuse the same name, so a
guard-created limiter can never carry a description in
`/ops/auth/rate-limits`. And keyset pagination is hand-rolled in every
module — including the trap that a placeholder appearing in no branch of the
statement has no type, which PostgreSQL refuses at run time.

## What this app does not exercise

Row-level security (`db/row_level_security.sql` is written but not applied —
[`examples/apps/invoicing`](../invoicing) shows it applied and tested, and
several of this app's queries deliberately cross organisations, which RLS
would have to bypass), organisation-overridable settings, retention policies,
storage on a real S3-compatible service, and email beyond what sign-in sends.
