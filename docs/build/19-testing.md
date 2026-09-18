# 19. Testing

Plateful's tests build the whole app — sign-in, organisations, `/ops`, every module, the real middleware stack — and send it HTTP requests, on a fresh PostgreSQL database per test. That sounds slow and expensive. It takes about 0.2 s per app on a laptop, and it is the only kind of test that can prove the things this guide has been claiming: that a stranger gets 404 rather than 403, that a refused order leaves no job behind, that a flag rolls out by organisation.

The tool is `gorbital.dev/gorbital/gorbitaltest`. The [testing with gorbitaltest guide](../guides/testing-with-gorbitaltest.md) lists every method; the [testing guide](../guides/testing.md) covers `pgtest`, the fakes for Google and Apple, and what CI runs. This chapter is about how Plateful uses them, and about three things that will waste your afternoon if nobody tells you.

## Run them

**What we're doing.** Pointing the tests at a PostgreSQL *server*, and making sure a green run means the tests actually ran.

**Why.** This is the single most likely thing to confuse a reader of this guide, so it goes first.

**What the framework already gives us.** `pgtest` creates a new `pgtest_…` database next to yours for each test, migrates it, and drops it at the end. It never touches your development data, which is why pointing it at your development server is safe and normal.

**What we build ourselves.** Two environment variables.

**How.** With `orb dev` running, or `docker compose up -d --wait`:

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://plateful:plateful@127.0.0.1:5432/plateful?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test -race ./...
```

**What just happened — and what happens if you skip the first line.** Without `GORBITAL_TEST_DATABASE_URL`, every test that needs a database **skips**. It does not fail. `go test ./...` prints `ok` for each package and exits 0. You will believe your app is tested, and the only thing that ran is the handful of tests that need no database.

The skip is not silent to `-v`: it says

```text
PostgreSQL for tests is not configured: run `docker compose up -d --wait` and set GORBITAL_TEST_DATABASE_URL (see compose.yaml)
```

but `go test ./...` without `-v` shows you nothing but `ok`.

> **Don't do this:** run `go test ./...`, see green, and believe it.
>
> **Do this instead:** set `GORBITAL_REQUIRE_DB=1` alongside the URL. It turns every one of those skips into a failure, so a misconfigured machine — or a misconfigured CI job — fails loudly instead of passing quietly. Put both lines in your CI configuration and in the README.

## Three traps

<a id="trap-principals"></a>

### 1. Tests don't sign in — they say who is calling

`gorbitaltest` puts a principal into the request's context exactly where an authenticator would, before the middleware stack runs. So there is no session, no password, no second factor:

```go
staff := app.As(gorbitaltest.User("usr_staff", "restaurants.restaurant.suspend"))
staff.Post("/v1/platform/restaurants/"+k.restaurantID+"/suspend", …)
```

`usr_staff` is not a row in `auth_users`. The permission list is what the caller holds; roles play no part. Guards see it, `actor.From(ctx)` in the use case sees it, audit events are attributed to it.

That is the right tool for testing a *guard*. It is the wrong tool whenever the test needs a caller who really exists — because an organisation's member is a row, and `guard.OrgMember` reads rows. For those, Plateful passes `gorbital.WithAuth(authhttp.New())` and uses `app.SignUp(t, "bruno@example.com")`, which registers the account, reads the verification code out of the queued email, signs in with a bearer token and hands back a client. Every test in `orders_test.go` uses real accounts for that reason:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-three-shapes -->

Bruno, Sara, the diner, the stranger, the rider and the other rider are six real accounts. The organisations are real. That is what makes the `org_not_found` assertions mean something.

<a id="trap-workers"></a>

### 2. Workers do not run

`gorbitaltest` builds the app but does not start River. **No job is ever worked, and no email is ever delivered.** This is a feature: a test that queued an email would otherwise have to wait for it.

What you do instead is read back what the app *stored*:

| Call | Returns |
|---|---|
| `app.Mail(t)` | Every email sent through `Deps.Mailer`, oldest first |
| `app.Jobs(t, kind)` | Every job of that kind enqueued through `Deps.Jobs`, with its JSON arguments; `""` for all |

It is also how `cmd/api/app_test.go` gets a session at all. That test builds the app with `main.go`'s options, which add Plateful's extra registration fields, so `app.SignUp` — which sends only an email and a password — cannot register there. The test has its own `signUp` helper that sends the extra fields and then reads the six-digit code out of the queued email:

```go
sent := app.Mail(t)
// …find the message for this address, pull six digits out of sent[i].Text
```

And it is how a test proves a notification was enqueued without a webhook ever being posted:

```go
queued := app.Jobs(t, notifications.FanoutJob)
```

To test what a job *does*, build its worker and call `Work` yourself — [below](#a-job-tested-by-calling-work).

<a id="trap-parallel"></a>

### 3. Do not use `t.Parallel()` in a package that builds apps

Huma keeps its error constructor in a package-level variable, and each app sets it to its own error mappings when it is built. Two apps built at once in one test binary race on it.

Tests in *different packages* run in separate processes and are unaffected, so `go test ./...` is as parallel as it needs to be: Plateful's eight modules test at the same time, each in its own binary, each with its own databases.

> **Don't do this:** add `t.Parallel()` to `TestPlacingAnOrderIsOneTransaction` because the suite feels slow.
>
> **Do this instead:** leave it out, and let package-level parallelism do the work. If a package really is slow, split the module's tests across files, not across goroutines.

## The layers

Three kinds of test, three costs, three things they can prove.

### Domain tests, with no database at all

**What we're doing.** Testing rules as plain functions.

**Why.** `domain` imports nothing but the standard library — the architecture test enforces it — so its rules can be tested with table tests that run in microseconds and never flake.

**How.** The order state machine ([chapter 8](08-orders-rules-in-the-domain.md)) is the clearest case. It walks the happy path, then tries every move the machine forbids:

<!-- include examples/apps/plateful/internal/modules/orders/domain/order_test.go#test-state-machine -->

**What just happened.** Every illegal transition in the app is now covered by one test that needs no PostgreSQL, no HTTP and no accounts. A second tap on "accept" cannot double-accept; a courier cannot deliver food the kitchen has not finished; a delivered order is terminal. `module.go` maps `ErrInvalidTransition` to 409 `invalid_order_transition`, so the HTTP behaviour follows from this test rather than being re-asserted in twenty handler tests.

Plateful's other domain tests are the same shape: `test-total-is-exact` (a total in integer minor units is exact to the penny, where the same sum in floating point loses one), `test-status-rules` in the restaurants module, `test-money-is-exact` in menus.

### Use-case tests

**What we're doing.** Testing an operation directly, without a route.

**Why.** Some operations have no route. `usecase.LateOrders` is only ever called by the `orders_late_sweep` job. A read model, a sweep, a reconciliation — none of them has a URL, and all of them have rules.

**How.** Build the service against the test app's real pool and call it. That is also how a *worker* is tested, since workers do not run: see the next section.

### Full request tests

**What we're doing.** Driving routes through the app exactly as a client does.

**Why.** Guards, middleware, error mappings, the OpenAPI validation layer, pagination and the deny-by-default rule only exist in the stack. A handler called directly proves nothing about them.

**How.** `cmd/api/app_test.go` builds the app with `main.go`'s own options and checks the platform's shape:

<!-- include examples/apps/plateful/cmd/api/app_test.go#test-app -->

**What just happened.** In one test: health and the OpenAPI document answer; the reviews list is the app's only route open to nobody in particular; everything else is 401; registration takes Plateful's own fields and refuses a request without them; a customer can browse but cannot reach `/ops` or `/v1/platform`; a restaurateur's own organisation is the only one they can see into; and `GET /v1/flags` lists the client flags and **not** the server-side one. That last assertion is a security test disguised as a feature test — a server-side flag key in a client response tells the world what you are about to ship.

## The tests that prove the claims

Everything this guide has asserted about Plateful is asserted again in code. These are the ones worth reading.

### One request, one transaction

[Chapter 9](09-one-transaction.md) said that placing an order writes the order, its lines, the stock the dishes take and the job that tells the restaurant, all together. Here is the proof, and note *how* it proves it — by making an order fail and showing that nothing at all was left behind:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-one-transaction -->

Stock went down by exactly what was ordered. The refused order left no row, no stock movement and no job. The job count is read with `app.Jobs` because, as above, workers do not run: the job row is the evidence.

<a id="three-shapes"></a>

### The three authorisation shapes

`TestThreeWaysToReachOneOrder` (included [above](#trap-principals)) is the module's reason for existing, and the proof of what [chapter 10](10-who-may-see-this-row.md) argued. It is the test to copy when you add a customer-facing route of your own. Read the assertions in pairs:

| Caller | Reaches it by | A caller who shouldn't gets |
|---|---|---|
| Restaurant staff | `guard.OrgMember` on `/v1/orgs/{orgId}/orders/{id}` | 404 `org_not_found` for another restaurant's organisation; 404 `order_not_found` for an order not in their own |
| The customer | `guard.Permission` plus an ownership check in the use case | 404 `order_not_found` — the same answer as for an order that does not exist, so IDs cannot be probed |
| The courier | Their courier profile, once the order is assigned to them | 404 `order_not_found` before assignment, and for a different courier |

Everything is 404, never 403. A 403 would confirm the order exists.

### Feature-flag targeting

[Chapter 14](14-settings-and-feature-flags.md) declared `orders.courier_auto_assign` as a server-side flag. A percentage rollout that flickers between two orders of the same restaurant is worse than no rollout. Plateful buckets on the *organisation* when the caller acts in one, so a percentage moves whole restaurants at a time — and this test rolls one restaurant in explicitly and leaves another out:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-flag-targeting -->

Bruno's kitchen gets a courier the moment an order is ready; Sara's does not. Same code, same request, different organisation.

### A guard and module middleware

Two different mechanisms, tested side by side: the module's own guard refuses an order at a restaurant that isn't accepting, *before* the handler runs; the module's middleware stops every customer write while an operator has `orders.ordering_paused` on, *before* sign-in and the guards:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-guard-and-middleware -->

The second test is also the proof that a runtime setting takes effect with nothing to deploy and nothing to push: `setSetting` writes through `/ops/settings`, and the very next request behaves differently.

### A job, tested by calling `Work`

<a id="a-job-tested-by-calling-work"></a>

Workers do not run ([chapter 13](13-background-jobs.md) built this one), so a job is tested by building its worker and calling `Work` directly. The notifier it is given here enqueues through the app's *real* job client, so what the sweep produced can be read back with `App.Jobs`:

<!-- include examples/apps/plateful/internal/modules/orders/orders_test.go#test-late-sweep -->

Three things this test establishes. A sweep with nothing late enqueues nothing — so the job is not noisy. A sweep with a late order enqueues exactly one notification. And `svc.LateOrders` can be called on its own, which is the point of the work living in a use case rather than in a method on the worker.

## What else is in the suite

| Test | Checks | Needs |
|---|---|---|
| `internal/modules/architecture_test.go` | The layer rules: `domain` imports only the standard library, `delivery` never imports `repository`, a module never imports another module's layers — only its root package | Nothing |
| `internal/modules/surface_test.go` | Error codes, audit actions, permissions, roles, settings, jobs and flags match `api/surface.json`. Nothing recorded may disappear ([chapter 17](17-extending-the-framework.md)) | Nothing |
| `cmd/api/main_test.go` | `api/openapi.json` is what the code describes, and `main.go` serves the commands it documents | Go |
| `internal/modules/*/[module]_test.go` | Each module's routes through the real stack: guards, errors, pages, versions, audit events | PostgreSQL |
| `internal/modules/*/domain/*_test.go` | The rules | Nothing |

Two of these fail in a way that looks alarming and isn't:

- **`TestPublicSurface` fails after you add a module.** It is telling you a new public name isn't recorded. Run `go test ./internal/modules -run TestPublicSurface -update` and commit `api/surface.json`. A *removed* name is a different story — that is a breaking change, and the test is right to stop you.
- **`TestOpenAPIIsCurrent` fails after you add a route.** Run `go run ./cmd/api openapi --dir api`.

Both are covered with their exact messages in [chapter 21](21-troubleshooting.md).

## Next

[20. Production and deployment](20-production-and-deployment.md): the image, the environment, and migrations as a release step.
