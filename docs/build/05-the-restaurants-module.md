# 5. The restaurants module

Plateful's first module, end to end. By the end of this chapter the organisations table carries a restaurant's fields — one table, not two — three kinds of caller reach it through three different guards, the error codes and permissions are declared in one file, and the platform's delivery-radius limit is a runtime setting an operator can change without a deploy.

Everything here is in [`internal/modules/restaurants`](../../examples/apps/plateful/internal/modules/restaurants). Read [chapter 4](04-sign-in-you-didnt-write.md) first if you haven't: accounts and organisations already exist when this chapter starts.

## 1. Generate the skeleton

**What we're doing.** Running one command that writes a table, four layers and a route table.

**Why.** A gorbital module has a fixed shape — four layers, one file per operation — and typing it out is tedious and easy to get subtly wrong, particularly the parts that keep one organisation's data away from another's.

**What the framework already gives us.** `orb gen module` writes the whole skeleton: the migration with its `CHECK` constraints and indexes, the domain type with validation, the use cases with an organisation check, the SQL with `org_id` in every statement, the HTTP handlers, the route table with guards, and a test file that already covers deny-by-default, cross-organisation isolation, pagination and optimistic locking. With `--org` it wires `guard.OrgMember` throughout.

**What we build ourselves.** Everything that makes a restaurant a restaurant: opening hours, a delivery radius, a suspension only the platform may apply, the two routes that let a diner browse — and the decision to keep all of it on the organisation's own row rather than in the table the generator writes.

**How.**

```bash
orb gen module Restaurant name:string:unique address:string 'cuisine:string?' \
  'status:enum(onboarding,open,paused,suspended)' --org
```

Read the pieces:

| Part | Means |
|---|---|
| `Restaurant` | Singular. The module is its plural: `internal/modules/restaurants`. The generator also writes a table `restaurants` with IDs prefixed `rst_`; Plateful replaces it, [below](#a-restaurant-is-its-organisation-one-table) |
| `name:string:unique` | 1–100 characters, required, sortable, unique. The first required string field becomes the title lists sort by |
| `'cuisine:string?'` | Optional. Quote it: `?` is a glob character in your shell |
| `status:enum(...)` | One of the values, the first by default; filterable |
| `--org` | Rows belong to an organisation. Routes go under `/v1/orgs/{orgId}/`, guarded by `guard.OrgMember`; the table gets `org_id text NOT NULL` referencing `orgs` |

`orb gen module` refuses to run on a dirty git tree, so the generated files arrive as a diff you can read. It also refuses to run at all on a `minimal`-preset app — [chapter 1](01-create-the-app.md) explains why that matters more than it sounds.

**What just happened.** Twenty-odd files, in the layout every gorbital module uses:

```text
internal/modules/restaurants/
├── module.go                 name, errors, permissions, settings, Routes: wires the layers
├── restaurants_test.go       HTTP tests through gorbitaltest
├── domain/                   the rules, in plain Go
├── usecase/                  one operation per file, and the port the repository implements
├── repository/               SQL
└── delivery/                 HTTP
db/migrations/<version>_restaurants.sql
```

The command does **not** edit `cmd/api/main.go`. It regenerates `internal/modules/modules.gen.go`, which `main.go` already reads through `modules.All()`. With `--org` it also tells you to add `orgshttp.Module(auth)` to `main.go` if it isn't there — `gorbital.New` refuses to start an app whose routes use `guard.OrgMember` without it, so you find out immediately rather than at runtime.

Field types are `string`, `string?`, `text` and `enum` and nothing else. Plateful's opening hours and delivery radius are integers, so they were added to the generated files by hand; the migration says so at the top. That is the normal way to use this generator: take the skeleton, then write the domain you actually have.

> [!NOTE]
> The generator is documented in full in [Generating code](../guides/generating-code.md#what-orb-gen-module-writes-file-by-file), including what each file contains and what `--org` changes. This chapter reads the finished module rather than repeating that page.

### A restaurant is its organisation: one table

The generated migration creates a `restaurants` table with its own `id` and an `org_id`. On Plateful every organisation runs exactly one restaurant, so that table would need `org_id NOT NULL UNIQUE` — and a one-to-one side table is one row split in two. Every reader would have to learn which half holds the name, which ID a route takes, and why an organisation and its restaurant can drift apart. The product owner put it more simply: an organisation *is* a restaurant.

So Plateful deletes the generated table and puts the restaurant's fields on `orgs`. The organisation's `id` is the restaurant's ID everywhere — in `/v1/restaurants/{id}`, in `orders.org_id`, in `restaurant_ratings` — and the organisation's `name` is the restaurant's name. That is possible because [chapter 4](04-sign-in-you-didnt-write.md) ejected the organisations module: its table is now your code, and a tenant's own fields belong on the tenant.

The columns arrive in a migration of their own, `db/migrations/20260918010020_orgs_restaurant.sql`:

<!-- include examples/apps/plateful/db/migrations/20260918010020_orgs_restaurant.sql#restaurant-columns -->

Read three decisions off it:

- **A new migration, not an edit to the ejected one.** `20260916000001_orgs.sql` has run on every database Plateful has — yours, CI's, production's. goose records it as applied and never runs it again, so a column added to that file would appear in new databases and silently never reach the existing ones. A migration that has run is history: you add the next one, you never rewrite it. (The same rule is why the organisations module keeps a byte-for-byte copy of that file.)
- **Every organisation has the columns; only a restaurant needs them filled.** A new account's personal workspace is an organisation too. `profile_created_at` stays `NULL` until the staff first save a profile, and the table-level `CHECK` requires an address only after that. The unique index on names is partial for the same reason: two personal workspaces may share a name, two restaurants may not.
- **The CHECK constraints match the domain's limits**, exactly as the generated table's did, so the database and `domain.validate` refuse the same values.

The repository says what "is a restaurant" means once, and every query of the module uses it:

<!-- include examples/apps/plateful/internal/modules/restaurants/repository/store.go#restaurant-columns-go -->

And there is no `INSERT`: the row has existed since the organisation was created, so creating a restaurant is an `UPDATE` that fills its columns in, once:

<!-- include examples/apps/plateful/internal/modules/restaurants/repository/create_restaurant.go#create-restaurant-sql -->

One row also means one version. Renaming the organisation through `PATCH /v1/orgs/{orgId}` and saving the restaurant's profile both change it, so each refuses to overwrite the other with a version conflict; and a rename to another restaurant's name answers `409 org_name_taken`, from the same unique index that gives the profile save `restaurant_name_taken`.

> **Don't do this:** keep a second table keyed by the tenant because the generator made one, or edit an applied migration because the file is right there.
> **Do this instead:** when a table is one-to-one with the organisation, add its columns to `orgs` with a new migration.

## 2. The four layers

**What we're doing.** Understanding which code belongs where before changing any of it.

**Why.** The layering isn't advice — `internal/modules/architecture_test.go` fails `go test` if you break it. Knowing the rule up front saves you from discovering it in CI.

**What the framework already gives us.** The architecture test, and a generated module that already obeys it.

**What we build ourselves.** The contents.

| Layer | Package | Imports | Holds |
|---|---|---|---|
| `domain` | `domain` | Only the standard library | What a restaurant is and what it may become. Its tests need no database |
| `usecase` | `usecase` | Its own `domain` | One operation per file. Owns the `Store` interface the repository implements, the permission names, and the audit actions |
| `repository` | `repository` | `usecase` and `domain` | Hand-written SQL, one file per operation |
| `delivery` | `delivery` | `usecase` and `domain` | HTTP: the route table, and one input/output/handler per operation |

`module.go` is the only file that knows all four, and the only one another package imports. No module imports another module's layers — the architecture test enforces that too, and it is the rule that makes Plateful's cross-module payment check awkward (the app's [README](../../examples/apps/plateful/README.md) explains where that leaves you).

The direction is one way: `delivery` calls `usecase` calls `domain`, and `repository` implements an interface `usecase` declares. Nothing points back up.

### domain: the rules, and nothing else

A `Restaurant` is a struct with a `Status`, and the statuses carry their own rule about who may set them:

<!-- include examples/apps/plateful/internal/modules/restaurants/domain/restaurant.go#restaurant-status -->

`SetByStaff` exists because `suspended` is the platform's, not the restaurant's. Putting it here rather than in the handler means every path into the domain gets the same answer — a route, a background job, a CLI command:

<!-- include examples/apps/plateful/internal/modules/restaurants/domain/restaurant.go#restaurant-apply -->

Two things worth copying. `Apply` returns the **names of the fields that changed**, so the use case can skip a write and the audit event can say what moved. And a suspended restaurant is frozen before anything else is checked: its staff cannot edit their way out of a suspension.

Opening hours are the sort of rule that looks trivial and isn't:

<!-- include examples/apps/plateful/internal/modules/restaurants/domain/restaurant.go#within-hours -->

A kitchen that closes before it opens works past midnight. That is one `switch` in the domain and a unit test that needs no database, instead of a bug that only appears at 00:05.

> **Don't do this:** put "is this restaurant open right now?" in the HTTP handler, or in the SQL.
> **Do this instead:** put it in `domain`, where the guard, the use case and the test can all reach the same function. Plateful's order guard calls `Accepting(now)`; so does the menu module, through its own query.

### usecase: one operation per file

`usecase/service.go` holds what every operation shares. First, the permission names — which are public API, so they are constants in one place:

<!-- include examples/apps/plateful/internal/modules/restaurants/usecase/service.go#restaurant-permissions -->

The comment is the whole multi-tenancy lesson in six lines: a permission is either an **organisation** permission (held through a role *inside* an organisation) or a **platform** permission (held through a platform role), never both.

Then one file per operation. Saving a profile:

<!-- include examples/apps/plateful/internal/modules/restaurants/usecase/save_restaurant.go#save-restaurant -->

Read the shape, because every write in Plateful has it: check who is calling, open a transaction, read the current row **locked**, ask the domain what the next row should be, write it, then record an audit event after the transaction commits. `memberID(ctx, orgID)` is the one line that turns "the guard let this request through" into "this actor is acting in this organisation"; without a `guard.OrgMember` on the route it returns `ErrUnauthenticated` rather than trusting the path.

And a customer-facing read, where the rule is *not* in a guard:

<!-- include examples/apps/plateful/internal/modules/restaurants/usecase/get_restaurant.go#view-restaurant -->

> **Don't do this:** reach for `guard.OrgMember` on a route a customer uses, because it is the guard you used everywhere else. A customer is a member of no organisation; the guard can only ever answer 404.
> **Do this instead:** guard the route with a platform permission the `user` role holds, and write the row-level rule in the use case — then test it, because nothing else will catch a missing comparison. Plateful's [Limits](../../examples/apps/plateful/README.md) section calls this the framework's sharpest edge, and it is.

Listing is where a second trap lives:

<!-- include examples/apps/plateful/internal/modules/restaurants/usecase/browse_restaurants.go#browse-restaurants -->

> **Don't do this:** store "which rows may this caller see" in the pagination cursor. A cursor from `gorbital.dev/page` is opaque, but it is **not signed**.
> **Do this instead:** apply the filter on every page, and let the cursor carry only position. The cursor does carry the sort it belongs to, so it can't be replayed against a different ordering.

### usecase: the port

The use cases never touch SQL. They declare the interface they need, and the repository implements it:

<!-- include examples/apps/plateful/internal/modules/restaurants/usecase/ports.go#restaurant-store-port -->

The interface lives in `usecase` and not in `repository` on purpose: the consumer owns the contract, so a use case test can substitute a fake, and the compiler checks the SQL layer against what the operations actually need.

`InTx` takes a `func(tx Store) error` — the same interface, bound to one transaction. That is how `SaveRestaurant` reads a locked row and writes it back without either layer knowing about `pgx`.

### repository: your SQL, by hand

<!-- include examples/apps/plateful/internal/modules/restaurants/repository/store.go#restaurant-store -->

One `Store` type serves both cases: on the pool, and on a transaction. Inside `InTx` the nested store has `pool == nil`, so calling `InTx` again simply joins the transaction already open rather than trying to start a second one.

The optimistic lock is the `WHERE` clause, not a lock:

<!-- include examples/apps/plateful/internal/modules/restaurants/repository/update_restaurant.go#update-restaurant-sql -->

If the stored version moved, no row matches, `CollectExactlyOneRow` reports no rows, and the caller gets `ErrRestaurantVersionConflict` → `409`. There is no read-modify-write race to reason about.

Constraint violations become domain errors here and nowhere else:

<!-- include examples/apps/plateful/internal/modules/restaurants/repository/store.go#restaurant-constraint-error -->

`postgres.UniqueViolation` gives you the **constraint name**, so each unique index maps to exactly the API error it means — even on a table two modules write: `orgs_restaurant_name` is `restaurant_name_taken` here and `org_name_taken` in the organisations module. [Chapter 6](06-migrations-and-the-database.md#7-constraint-violations-are-typed-errors) covers the full set of these helpers.

> **Don't do this:** return the driver's error to the caller, or match on its text.
> **Do this instead:** map the constraints your use cases handle, and let everything else be wrapped and hidden — `storeError` in `service.go` returns the module's own errors unchanged and turns anything else into an opaque message, because a driver error is not API.

### delivery: HTTP, and nothing else

The whole route table is one file, so what a module exposes and who may call it reads in one screen:

<!-- include examples/apps/plateful/internal/modules/restaurants/delivery/routes.go#restaurant-routes -->

Three groups, three guards, and the path tells you which is which:

- `/v1/orgs/{orgId}/restaurant` — `guard.OrgMember(perm)`. The guard asks the organisations module about `{orgId}` **before the body is read**. A non-member, an unknown organisation and a malformed ID all get `404 org_not_found`, so IDs can't be probed; a member whose role lacks the permission gets `403 forbidden`.
- `/v1/restaurants` — `guard.Permission(PermBrowse)`, a platform permission the `user` role grants to every signed-in account.
- `/v1/platform/restaurants` — `guard.Permission(PermSuspend)`, plus `guard.RecentReauth()` on the two destructive routes, so suspending a business needs a recently proved session and not just a long-lived token.

A handler is four lines of translation and no decisions:

<!-- include examples/apps/plateful/internal/modules/restaurants/delivery/save_restaurant.go#save-restaurant-handler -->

> **Don't do this:** decide in the handler. Not "may this caller set `suspended`?", not "is the radius too big?", not "does this organisation already have a restaurant?".
> **Do this instead:** read the request, call one use case, shape the answer. The handler above doesn't even choose a default status beyond mapping an empty string — the rule about *who* may set a status is in `domain.Apply`, where a job or a command would hit it too.

The struct tags are not decoration: `minLength`, `maxLength`, `minimum`, `enum` and `example` are checked by Huma on every request, before your code runs, and they appear in the OpenAPI document. Validation you can express in a tag should be in a tag — but the domain validates again anyway, because a use case must be correct when called from a job that never saw an HTTP request.

## 3. `module.go`: what a module declares

**What we're doing.** Reading the one file that connects a module to the app.

**Why.** Error codes, permission names and setting keys are the module's public API. They live in one file so that adding one is obvious and changing one is obviously a breaking change.

**What the framework already gives us.** `gorbital.Module` is a plain struct. The library reads it to build the route table, the error mapping, the permission catalogue, the settings registry and the OpenAPI document — and to answer `/ops/settings`, `/ops/auth/rate-limits` and the rest.

**What we build ourselves.** The values.

<!-- include examples/apps/plateful/internal/modules/restaurants/module.go#module -->

### Errors

<!-- include examples/apps/plateful/internal/modules/restaurants/module.go#errors -->

Each mapping says: when a use case returns this Go error, answer with this status and this stable `code`. Clients branch on `code`, never on `detail` or on the status alone. Note what is **absent**: `org_not_found` and `forbidden` aren't here, because `guard.OrgMember` answers them itself before the use case runs.

The domain errors themselves are ordinary sentinels in `domain/errors.go`, and `ValidationError` unwraps to `ErrInvalidRestaurant`, so one mapping covers every invalid field while `delivery/responses.go` turns the field list into `errors[]` in the problem document.

### Permissions

<!-- include examples/apps/plateful/internal/modules/restaurants/module.go#permissions -->

Two catalogues in one list, told apart by which field is set:

- `OrgRoles` — held **inside** an organisation, by a member with that role. `guard.OrgMember` checks them.
- `Roles` — platform roles. `user` is held by every signed-in account without any grant, which is how a customer who belongs to nothing is allowed to browse. `platform_admin` and `ops_viewer` are granted by an operator with `go run ./cmd/api grant-role <email> <role>`.

A permission belongs to exactly one catalogue. To stop ordinary members editing the profile, you delete `"member"` from `PermWrite`'s `OrgRoles` — there is no second place to remember.

### Settings

<!-- include examples/apps/plateful/internal/modules/restaurants/module.go#settings -->

**What this buys you.** The platform's maximum delivery radius is now a row in the database, editable through `PUT /ops/settings/restaurants.max_delivery_radius_m`, with a range the API enforces, a description operators can read, and — because of `ReasonRequired()` — a mandatory reason recorded in the setting's history. No deploy, no restart, and a full audit trail. That is `settings.Int` plus five lines.

Two details worth copying:

- The `*settings.Setting[int]` is captured in a variable declared **before** the `gorbital.Module` literal and used in `Routes`, so the service holds the typed handle. There is no lookup by string at call time and no chance of a typo in a key.
- `Service.limit(ctx)` reads it on **every write**, not at start-up. Change the limit at 11am and the next save obeys it; restaurants already above the new limit keep what they have until they next save. That behaviour is a sentence in the setting's own description, which is where an operator will look for it.

`Routes` is the last field, and the only one that runs at build time:

```go
Routes: func(r *gorbital.Router, d gorbital.Deps) {
	svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger, maxRadius)
	delivery.Register(r, svc)
},
```

`gorbital.Deps` carries the pool, the audit recorder, the logger and the rest. Note the comment in the source: `d` is **zero** while the OpenAPI document is being exported (`go run ./cmd/api openapi`), because that command describes the code and never opens a database. Constructors must therefore tolerate nil dependencies — `NewService` does, and `storeError` never dereferences one.

## 4. The test that proves the three callers

<!-- include examples/apps/plateful/internal/modules/restaurants/restaurants_test.go#test-three-callers -->

`gorbitaltest.New` builds the real app — real middleware, real guards, real migrations — on a database of its own for this test. `app.SignUp` creates a real account; `app.As(gorbitaltest.User(...))` fabricates a principal with named permissions, which is how platform staff are simulated without granting a role.

Every assertion in it is a rule from this chapter: staff see only their own organisation, a customer sees only what is open, an anonymous request is refused, and `/v1/platform/*` is closed to both. The suspension test next to it proves the other half — that a suspension takes effect immediately, because the orders module reads the restaurant's status on every order placed rather than being told about it.

[Testing with gorbitaltest](../guides/testing-with-gorbitaltest.md) is the reference for the harness; [chapter 6](06-migrations-and-the-database.md#8-a-database-per-test) explains where the per-test database comes from.

## What just happened

You ran one command and then wrote a domain. The module now has: its columns on the organisations table, added by a migration of its own, with constraints that match its Go validation; five permissions across two catalogues; eight error codes; a runtime setting with a range, a description and a required reason; audit events for every change; cursor pagination; optimistic locking; and three kinds of caller reaching one table through three different doors — with the difference visible in `delivery/routes.go`, in one screen.

What gorbital did *not* do: decide what a restaurant is, write a line of your SQL, or notice that `ViewRestaurant` must check `Status == open`. That check is one `if` in a use case, and only the test on the previous page stands between it and an information leak.

Next: [chapter 6](06-migrations-and-the-database.md) goes under the module — the migration that added its columns, the single history it joins, and the database tools the repository layer uses.
