# Generating code

In an app on `gorbital.Main`, `orb gen module` writes a new module and `orb gen middleware` writes middleware or a guard. What they write is ordinary Go that you own from then on: no base types, no reflection, no generated file you mustn't touch except `modules.gen.go`. This guide goes through what each generator writes, why the module layout is what it is, and how to add an operation by hand. Flags, JSON output and exit codes are in the [CLI guide](cli.md#orb-gen-module); the decision is [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md#phase-8-implementation-notes-2026-09-17).

| Command | Writes |
|---|---|
| `orb gen module Shelf name:string:unique …` | `internal/modules/shelves/` (a layered module), its migration, and `modules.gen.go` |
| `orb gen middleware RequireClientVersion --module books` | A middleware and its test in the module's `delivery/` |
| `orb gen middleware ActiveSubscription --module books --guard` | A guard, its error and its test in the module's `delivery/` |
| `orb gen middleware TenantHeader --global` | A middleware and its test in `internal/middleware/` |
| `orb gen modules` | `internal/modules/modules.gen.go`, the module list (`orb dev` runs it) |
| `orb routes` | Nothing: it lists every route with its guards and source |

Every generator plans before it writes: `--dry-run` prints the plan, `--diff` prints it as a unified diff, and the Dev Portal's [generators hub](../dev-portal/generators.md) shows the same plan before Apply. A plan only **creates** files, apart from `modules.gen.go`: it refuses when a file or the module's directory exists, and applying refuses when a file changed since the plan was made. Running a generator twice never overwrites your code.

## The module layout

```text
internal/modules/shelves/
├── module.go                 name, errors, permissions, Routes: wires the layers
├── shelves_test.go           HTTP tests through gorbitaltest
├── domain/
│   ├── shelf.go              Shelf, ShelfFields, Changes, NewShelf, Apply, validation
│   ├── errors.go             sentinel errors and ValidationError
│   └── shelf_test.go
├── usecase/
│   ├── service.go            Service, permissions, audit actions, owner check, audit helper
│   ├── ports.go              the Store interface the repository implements
│   ├── create_shelf.go
│   ├── get_shelf.go
│   ├── list_shelves.go
│   ├── update_shelf.go
│   └── delete_shelf.go
├── repository/
│   ├── store.go              Store on the pool or a transaction, columns, scanner, constraint errors
│   ├── insert_shelf.go
│   ├── select_shelf.go
│   ├── select_shelves.go
│   ├── update_shelf.go
│   └── delete_shelf.go
└── delivery/
    ├── routes.go             the whole route table, with guards
    ├── responses.go          ShelfResponse, the path input, field errors
    ├── create_shelf.go       input, output and handler
    ├── get_shelf.go
    ├── list_shelves.go
    ├── update_shelf.go
    └── delete_shelf.go
db/migrations/20260920000002_shelves.sql
```

**Four layers**, as in every module ([ADR-0039](../adr/0039-resource-module-template.md)): `domain` holds the rules in plain Go and imports only the standard library, so its tests need nothing; `usecase` runs one operation and owns the port the repository implements; `repository` is SQL; `delivery` is HTTP. `module.go` is the only place that knows all four. `internal/modules/architecture_test.go` checks it on every `go test`: the domain imports nothing of the app, the use cases only their domain, the repository and delivery only the use cases and the domain, and no module imports another.

**One file per operation** in `usecase/`, `repository/` and `delivery/`. A change to "update a shelf" touches three files named `update_shelf.go` and nothing else, a review reads one operation at a time, and two people adding different operations don't edit the same file. The layers are separate packages, so the three `update_shelf.go` can't collide, which is why the flat layout (`handlers.go`, `store.go` in one package) was dropped.

**The route table stays in one file.** `delivery/routes.go` lists every path, method, operation ID, status and guard, so what a module exposes and who may call it reads in one screen; the handlers it names are in the operation files.

## What `orb gen module` writes, file by file

The examples are Shelfie's shelves module, which `orb gen module Shelf name:string:unique description:text 'visibility:enum(private,shared)' --plural Shelves` writes unchanged ([chapter 9](../examples/shelfie/09-generators.md)).

### `module.go`

`func Module() gorbital.Module`: the module's `Name` (the package), its `Errors` (each domain error with a status, a stable code and a detail), its `Permissions` (`shelves.shelf.read` and `.write`, held by the `user` role every user has, so sessions pass and an API key only within its scopes) and `Routes`, which builds the store on `Deps.DB`, the service with `Deps.Audit` and `Deps.Logger`, and registers the routes. While the OpenAPI document is exported, `Deps` is zero: the service is built but no use case runs.

Error codes and permission names are public API: add new ones, don't rename them.

### `domain/`

| Name | What it is |
|---|---|
| `Shelf` | The record: `ID`, `OwnerID`, the fields in `ShelfFields`, `Version`, `CreatedAt`, `UpdatedAt` |
| `NewShelf(id, ownerID, fields, now)` | Trims text, applies enum defaults, validates every field and returns a `*ValidationError` listing each invalid one |
| `Changes`, `(Shelf) Apply(changes, now)` | A partial update: nil fields keep their value; returns the names of the fields that changed, so an update that changes nothing writes nothing and records no audit event |
| `Visibility` | One type per enum field, with its constants and `Valid` |
| `MaxNameLength`, … | The limits the migration's `CHECK` constraints repeat |
| `errors.go` | `ErrUnauthenticated`, `ErrInvalidShelf` (what `ValidationError` unwraps to), `ErrShelfNotFound`, `ErrShelfNameTaken` per unique field, `ErrShelfVersionConflict` |

`shelf_test.go` tests every rule with tables: blank and too-long text, NUL characters, unknown enum values, every field invalid at once, and `Apply`.

### `usecase/`

`service.go` holds what every operation shares: `NewService(store, recorder, logger)`, the permission names and audit actions, IDs (`shl_` and 128 random bits), a clock at PostgreSQL's precision, `ownerID` (the signed-in user; service accounts own nothing and get `ErrUnauthenticated`), the audit helper (after the change; a failed audit write is logged, not returned) and `storeError`, which passes the module's errors through and hides driver errors. `ports.go` is the `Store` interface, the `ListQuery` and the page `Position`.

| File | Operation |
|---|---|
| `create_shelf.go` | `CreateShelf`: validates, inserts, records `shelves.shelf.created` |
| `get_shelf.go` | `GetShelf`: one of the user's shelves, or `ErrShelfNotFound`, also for someone else's |
| `list_shelves.go` | `ListShelves`: keyset pages with `page.Params` (`limit`, `cursor`, `sort` on `created_at`, `updated_at` and each string field, newest first by default) and a filter per enum field. The cursor records its sort, so it can't be replayed with another |
| `update_shelf.go` | `UpdateShelf`: in one transaction, locks the row, checks the `version` the caller read (`ErrShelfVersionConflict` otherwise), applies the changes, saves and records the changed field names, never their values |
| `delete_shelf.go` | `DeleteShelf`: a hard delete, recorded |

Use cases don't check permissions: the route's guard did, before the body was read. They do check who the owner is.

### `repository/`

`store.go` has `NewStore(pool)`, `InTx` (a store bound to one transaction; inside one, it joins it), `shelfColumns` and `scanShelf`, and `constraintError`, which turns the unique index `shelves_owner_name` into `ErrShelfNameTaken`. Then one statement per file; every statement filters on `owner_id`, and every value is a placeholder. `select_shelves.go` holds one fixed query per allowed sort: text sorts compare `lower(field) COLLATE "C"`, so the order doesn't depend on the database's locale.

### `delivery/`

`routes.go`:

```go
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	shelves := r.Group("/v1/shelves", gorbital.Tags("Shelves"))

	gorbital.Post(shelves, "", h.createShelf, gorbital.OperationID("shelves-create"),
		gorbital.Summary("Create a shelf"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))
	// list, get, update and delete follow
}
```

Every route requires a signed-in caller (no `guard.Public()`) and the read or write permission. `responses.go` has `ShelfResponse` (with `version`, which clients send back when updating), the `{id}` path input, and `fieldErrors`, which turns a `ValidationError` into a 422 `validation_failed` problem with one `errors[]` entry per field (`body.name`, `query.visibility`). Each operation file has its input type (Huma validates lengths, enums and required fields before the handler), its output and its handler, which only maps between HTTP and the use case.

### The tests and the migration

`shelves_test.go` runs the module in a real app on a new database per test ([gorbitaltest](testing-with-gorbitaltest.md)): create and get; the rules (blank and missing title, unknown enum values, a unique value in other capitals); deny by default (401), another user's shelf (404 on get, update and delete, and an empty list), a read-only API key (403 on writes); pages sorted by the title with a cursor, an invalid cursor and sort, the enum filter; versions (a missing version is 422, a stale one 409); delete; and the audit trail.

The migration creates the table with a `CHECK` per field limit and enum, a unique index per unique field on `(owner_id, lower(field))`, and an index per sort led by `owner_id`, plus `-- +goose Down`. `owner_id` has no foreign key: an app on `gorbital.Main` may not have the sign-in tables. Change the migration freely until it is released; afterwards, add a new one with `orb gen migration`.

### Organisations

`--org` is refused until organisations arrive in the library with `guard.OrgMember` (v0.2 Phase 7). Generate the module owned by users; the org-scoped variant will scope the routes under `/v1/orgs/{orgId}/…` with the guard.

## Adding an operation by hand

Say shelves can be archived: `POST /v1/shelves/{id}/archive`. One file per layer, plus a line in the route table:

1. **Domain** — the rule, in `domain/shelf.go` (or a new `domain/archive.go`):

   ```go
   // Archive returns s archived at now, or ErrShelfArchived.
   func (s Shelf) Archive(now time.Time) (Shelf, error) { … }
   ```

   Add `ErrShelfArchived` to `errors.go`, a test to `shelf_test.go`, and a column in a new migration (`orb gen migration add_shelf_archived_at`).

2. **Port** — a method on `Store` in `usecase/ports.go`:

   ```go
   // ArchiveShelf saves the archived time when the version is still s.Version.
   ArchiveShelf(ctx context.Context, s domain.Shelf) (domain.Shelf, error)
   ```

3. **Repository** — `repository/archive_shelf.go`: one `UPDATE … WHERE id = $1 AND owner_id = $2 AND version = $3 RETURNING` and the method. The compile-time check `var _ usecase.Store = (*Store)(nil)` in `store.go` tells you until it exists.

4. **Use case** — `usecase/archive_shelf.go`: `ownerID`, load, `Archive`, save, audit `shelves.shelf.archived` (a new constant in `service.go`), and add `ErrShelfArchived` to `storeError`'s list.

5. **Delivery** — `delivery/archive_shelf.go` with its input (`ID` from the path, `Version` in the body) and handler, and one line in `routes.go`:

   ```go
   gorbital.Post(shelves, "/{id}/archive", h.archiveShelf, gorbital.OperationID("shelves-archive"),
   	gorbital.Summary("Archive a shelf"), gorbital.Errors(http.StatusNotFound, http.StatusConflict),
   	guard.Permission(usecase.PermWrite))
   ```

6. **Module** — map `ErrShelfArchived` in `module.go` (`409 shelf_archived`), then a test in `shelves_test.go`, `go run ./cmd/api openapi --dir api`, and `orb routes --module shelves` to see it listed with its guard.

## Middleware and guards

`orb gen middleware` writes the shape of [Guards and middleware](guards-and-middleware.md) with the rule left for you, and a table-driven test to extend with the requests the rule refuses.

| Kind | The function | Its rule | Its test |
|---|---|---|---|
| Module middleware | `func RequireClientVersion(next http.Handler) http.Handler` in `delivery/` | `checkRequireClientVersion(r) *httpx.Problem`: nil lets the request through; a problem is written with `httpx.WriteProblem` and `next` never runs | Wraps a handler with `httptest` and checks the status and whether `next` ran |
| Guard | `func ActiveSubscription() gorbital.RouteOption` in `delivery/`, `guard.New` named `active_subscription` with status 403 | `checkActiveSubscription(ctx, req guard.Request) error`: nil, or `ErrActiveSubscriptionRefused` | Mounts a test module with the guard and the error mapped, and sends signed-in and anonymous requests |
| Global middleware | `func TenantHeader(next http.Handler) http.Handler` in `internal/middleware` | As module middleware | As module middleware |

Until you write the rule every request passes, so wiring the generated code in changes nothing by itself. Where each runs: module middleware (`gorbital.Use`, `Module.Middleware`) before the sign-in check and guards, so for anonymous callers too; global middleware (`gorbital.WithMiddleware`) after the built-in stack, so after authentication; guards after the sign-in check, before the body is read.

## Checking the result

- `go test ./...` runs the generated tests and the architecture test.
- `orb routes` lists the new routes with their guards and file:line, next to the library modules' routes (`--app` lists only yours); `orb routes --public` shows what needs no sign-in.
- `orb doctor` reports a module directory missing from `modules.gen.go`, a custom stack without `Recover` or `Auth`, and pending migrations.

## Not generated

Relations between modules (reference another module's IDs in SQL; modules never import each other), soft delete, search, file fields, org-scoped modules (Phase 7), and an sqlc-based repository variant (a possible later option, noted in ADR-0083).
