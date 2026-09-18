# 0. What we're building

This guide builds **Plateful**, a restaurant delivery platform, from `orb new` to a tested, deployable API with eight modules of its own. The finished code is in [`examples/apps/plateful`](../../examples/apps/plateful), which CI builds and tests; every block of code on these pages is included from that directory rather than retyped, so the text cannot drift from code that compiles.

Read this chapter to decide whether the guide is for you. It shows what the finished thing does, names the parts gorbital gives you and the parts you write, and says plainly what the guide does not cover.

## The domain

One platform. Many restaurants. **Each restaurant is an organisation** — an account can be a member of one, with a role in it, and every row of that restaurant's data carries its `org_id`.

That much is an ordinary multi-tenant API, and gorbital has a shape for it. What makes Plateful worth a guide of this length is the third kind of caller:

| Who | What they are | How a route lets them in |
|---|---|---|
| **Platform staff** | Accounts holding the `platform_admin` or `ops_viewer` platform role | `/ops/*`, and `/v1/platform/*` with `guard.Permission` |
| **Restaurant staff** | Members of a restaurant's organisation, with the role `owner`, `admin` or `member` | `/v1/orgs/{orgId}/*` with `guard.OrgMember` |
| **Customers and couriers** | Ordinary signed-in accounts that belong to **no organisation at all** | `/v1/*` with `guard.Permission` on a permission the `user` role holds, plus an ownership check written in the use case |

A diner orders from a restaurant they are not a member of. A courier delivers for many restaurants and is employed by none. `guard.OrgMember` is the wrong tool for both, and there is no role that means "the customer of *this* order". Most of the interesting code in Plateful — and most of the sharp edges this guide points at — comes from that third row.

The modules Plateful ends up with:

| Module | What it is |
|---|---|
| `restaurants` | The tenant's own profile, and its status. [Chapter 5](05-the-restaurants-module.md) builds it end to end |
| `menus` | Sections, dishes, prices, stock |
| `orders` | The heart: a state machine from `placed` to `delivered` |
| `couriers` | A courier's profile and availability — the one table with no `org_id` |
| `payments` | A payment per order, confirmed by a provider's webhook |
| `reviews` | A rating for a delivered order, and the app's only public route |
| `images` | Menu photos and restaurant covers, through `gorbital.dev/modules/storage` |
| `notifications` | Outbound webhooks to a restaurant's chat — a thing the framework does **not** have, added as a module of the app's own |

## Three requests, three kinds of caller

These are real routes of the finished app. The full route table is in its exported OpenAPI document, [`api/openapi.json`](../../examples/apps/plateful/api/openapi.json); the field names below come from the handlers, and the values are made up.

**A restaurateur publishes their restaurant.** The organisation is in the path, and `guard.OrgMember` decides whether this account is staff of it:

```bash
curl -X PUT http://localhost:8080/v1/orgs/org_mfrggzdfmztwq2lkmfrggzdfmy/restaurant \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"version": 1, "name": "Trattoria Bruno", "address": "12 Market Street, Leeds",
       "cuisine": "Neapolitan", "opens_minute": 660, "closes_minute": 1320,
       "delivery_radius_m": 3000, "status": "open"}'
```

```json
{
  "id": "rst_mfrggzdfmztwq2lkmfrggzdfmy",
  "name": "Trattoria Bruno",
  "address": "12 Market Street, Leeds",
  "cuisine": "Neapolitan",
  "opens_minute": 660,
  "closes_minute": 1320,
  "delivery_radius_m": 3000,
  "status": "open",
  "created_by": "usr_mfrggzdfmztwq2lkmfrggzdfmy",
  "version": 2,
  "created_at": "2026-09-18T09:14:02.118431Z",
  "updated_at": "2026-09-18T09:31:44.007210Z"
}
```

`version` is how a second editor is stopped from silently overwriting the first: you send the version you read, and the update matches on it. Send a stale one and the answer is `409 restaurant_version_conflict`.

**A diner browses.** No organisation anywhere in the path, because the diner is a member of none:

```bash
curl "http://localhost:8080/v1/restaurants?cuisine=Neapolitan&limit=20" \
  -H "Authorization: Bearer $TOKEN"
```

```json
{
  "items": [
    {
      "id": "rst_mfrggzdfmztwq2lkmfrggzdfmy",
      "name": "Trattoria Bruno",
      "address": "12 Market Street, Leeds",
      "cuisine": "Neapolitan",
      "opens_minute": 660,
      "closes_minute": 1320,
      "delivery_radius_m": 3000,
      "status": "open",
      "created_by": "usr_mfrggzdfmztwq2lkmfrggzdfmy",
      "version": 2,
      "created_at": "2026-09-18T09:14:02.118431Z",
      "updated_at": "2026-09-18T09:31:44.007210Z"
    }
  ],
  "next_cursor": "eyJzIjoibmFtZSIsIngiOiJUcmF0dG9yaWEgQnJ1bm8iLCJpIjoicnN0XzEifQ"
}
```

Only restaurants that are `open` are in that list, and a restaurant that isn't answers `404 restaurant_not_found` when asked for by ID — the same answer as an ID that never existed, so a suspension cannot be discovered by probing.

**Something goes wrong.** Every error in a gorbital app is [RFC 9457 problem JSON](../guides/error-handling.md), with a stable `code` your client can branch on:

```json
{
  "title": "Conflict",
  "status": 409,
  "code": "restaurant_version_conflict",
  "detail": "the profile changed since you read it; get it again and retry",
  "request_id": "req_9f86d081884c7d65"
}
```

The codes are declared by the module that owns them, next to the Go errors they map from; [chapter 5](05-the-restaurants-module.md) shows the declaration.

## The three callers, in a test

The app's own top-level test is the shortest honest description of the finished platform. It builds the app exactly as `main.go` does, on a fresh database, and walks all three callers through it:

<!-- include examples/apps/plateful/cmd/api/app_test.go#test-app -->

Notice what it asserts and does not have to arrange: anonymous requests are refused everywhere but one route, because gorbital's default is deny; `/ops` is closed to a customer without the app writing a line of code for it; and a customer asking about somebody else's organisation gets `404 org_not_found`, not `403`, so the organisation's existence isn't leaked. Those are framework behaviours. Which restaurants a diner may *see* is Plateful's own rule, and lives in a use case.

## What the framework gives you, and what you write

This distinction is the point of the whole guide, so here it is once, up front. Everything in the left column arrives with `orb new --preset full`; everything in the right is code you write in `internal/modules/`.

| gorbital gives you | You write |
|---|---|
| Sign-in: registration, email verification, sessions, password reset, 2FA, passkeys, Google/Apple/GitHub, API keys — 66 operations ([chapter 4](04-sign-in-you-didnt-write.md)) | The extra fields your registration form needs, as a struct |
| Organisations: members, roles, invitations, personal workspaces, service accounts | What a restaurant *is*: its profile, hours and status |
| `guard.OrgMember`, `guard.Permission`, `guard.RateLimit`, `guard.Webhook`, `guard.RecentReauth`, `guard.Public` | The rule that this order belongs to this customer |
| Migrations, the connection pool, transactions, typed constraint errors ([chapter 6](06-migrations-and-the-database.md)) | Your tables and every line of your SQL |
| Background jobs, runtime settings, feature flags, the audit log, `/ops/*`, health checks, OpenAPI, telemetry | The jobs, settings, flags and audit actions your modules declare |
| Problem+JSON errors, request IDs, the middleware stack, CORS, security headers, body limits | The error codes your module maps |
| A test harness that gives each test its own database and real signed-in accounts | The tests |

gorbital is a **library you import**, not a framework that owns your process. `cmd/api/main.go` is a list of options you can read in one sitting; your modules are ordinary Go packages. [Chapter 2](02-framework-and-your-app.md) draws that boundary properly.

## What this guide does not cover

Honesty is cheaper than a wasted afternoon:

- **No frontend, of any kind.** gorbital builds HTTP APIs. There is no template engine, no asset pipeline, no server-rendered HTML. Plateful has no web app; the "customer app" in these pages is a client you would write separately.
- **No admin UI.** `/ops/*` is a set of JSON APIs for operators — settings, flags, jobs, the audit log, accounts — and nothing renders them. The [Dev Portal](../guides/dev-portal.md) that `orb dev` opens is a **development** tool on your own machine, not something you deploy.
- **No mobile client generation.** The app exports an OpenAPI document, a Postman collection and an `llms.txt`; turning those into a Swift or Kotlin client is your generator's job, not gorbital's. The framework does help you with the *server* side of mobile sign-in (passkeys with app IDs, Google and Apple ID tokens), which [chapter 4](04-sign-in-you-didnt-write.md) points at.
- **Not every line of every module.** The chapters build `restaurants` in full and then work outward, taking the one lesson each later module teaches best. The rest of their code is in the repository, and the app's own [README](../../examples/apps/plateful/README.md) is a map of it.
- **Row-level security is written but not applied.** Plateful ships `db/row_level_security.sql` unapplied, because several of its queries deliberately cross organisations. The [multi-tenant invoicing recipe](../examples/recipes/multi-tenant-invoicing.md) shows RLS applied and tested instead.

One more piece of honesty, because it shapes chapters 5 and 10: gorbital has **no primitive between "signed in" and "member of this organisation"**. For a customer or a courier, the authorisation rule is ordinary Go inside a use case, invisible to the route table and to any audit of guards. The app's README has a [Limits](../../examples/apps/plateful/README.md) section listing this and eight other places the framework fought back. This guide does not hide them.

## What you need

[Go 1.26 or later, Docker, and git](../start/prerequisites.md), plus an authenticator app for the administrator's 2FA. `curl` and `jq` make the examples easier to follow; your app's `/docs` page has a **Try it** button on every endpoint if you'd rather click.

Then the CLI:

```bash
go install gorbital.dev/cli/cmd/orb@latest
orb version
```

You do **not** need to know gorbital already. You do need to be comfortable with Go — this guide teaches a framework, not a language — and to have seen SQL before, because you write all of yours by hand.

If you want a smaller first app, [Shelfie](../examples/shelfie/00-start-a-project.md) is a single-tenant reading tracker built the same way in eleven chapters. It overlaps this guide deliberately; where a topic is covered better there, these pages link to it rather than repeat it.

## The chapters

| Chapter | What it covers |
|---|---|
| 0. What we're building | This page |
| [1. Create the app](01-create-the-app.md) | `orb new`, why `--preset full` is not optional, and the tree it writes |
| [2. The framework and your app](02-framework-and-your-app.md) | Where the library ends and your code begins |
| [3. Configuration and the first run](03-configuration-and-first-run.md) | `.env`, what `orb dev` does in order, the credentials it prints once |
| [4. Sign-in, in your repository](04-sign-in-you-didnt-write.md) | What `authhttp.New()` gives you, the options, the four hooks, and ejecting |
| [5. The restaurants module](05-the-restaurants-module.md) | `orb gen module --org`, the four layers, errors, permissions and settings |
| [6. Migrations and the database](06-migrations-and-the-database.md) | One merged history, forward-only migrations, `InTx`, constraint helpers, `pgtest` |
| [7. The menu, money and photos](07-the-menu-money-and-photos.md) | Money as integer minor units, and uploads through `modules/storage` |
| [8. Orders, and rules in the domain](08-orders-rules-in-the-domain.md) | A state machine that belongs nowhere else |
| [9. One transaction](09-one-transaction.md) | Four tables and a queued job, committed or rolled back together |
| [10. Who may see this row](10-who-may-see-this-row.md) | The three ownership shapes, and where the framework stops helping |
| [11. Validation and errors](11-validation-and-errors.md) | A refusal from the layer that raises it to the JSON a client reads |
| [12. Your own function](12-your-own-function.md) | Guards and middleware of your own |
| [13. Work that happens without a request](13-background-jobs.md) | Jobs, schedules, and a use case with no route |
| [14. Settings and feature flags](14-settings-and-feature-flags.md) | Changing behaviour without a deploy, and rolling a flag out by organisation |
| [15. Paying for an order](15-payments-a-rule-across-modules.md) | A provider webhook, idempotency, and a rule spanning two modules |
| [16. Reviews, and the one route that needs nobody](16-reviews-and-the-public-route.md) | `guard.Public`, and a third ownership shape |
| [17. Extending the framework](17-extending-the-framework.md) | Outbound webhooks, which gorbital does not have |
| [18. Audit, logs and observability](18-audit-logs-and-observability.md) | The audit log, structured logs, traces and metrics |
| [19. Testing](19-testing.md) | `gorbitaltest`, and what is worth asserting |
| [20. Production and deployment](20-production-and-deployment.md) | The image, the environment, migrations as a release step |
| [21. Troubleshooting](21-troubleshooting.md) | The failures you will actually hit |
| [22. Where to go from here](22-where-to-go-from-here.md) | What to read next |

Start with [chapter 1](01-create-the-app.md).
