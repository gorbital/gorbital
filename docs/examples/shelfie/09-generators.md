# 9. Generators

Readers want named shelves: "Summer 2026", "To lend Ada". A shelf has a name, a description and a visibility, belongs to one reader, and needs the same things the books module has: rules, SQL, routes with guards, tests and a migration. `orb gen module` writes all of it, in the layout of [chapter 1](01-books-module.md), and this chapter's shelves module is exactly what it wrote. Then `orb routes` shows what the app serves, and `orb gen middleware` starts a middleware or a guard ([Generating code](../../guides/generating-code.md)).

## Generate the module

In `examples/apps/shelfie`, with a clean git tree:

```bash
orb gen module Shelf name:string:unique description:text 'visibility:enum(private,shared)' --plural Shelves --dry-run
```

```text
✓ Would create (dry run) module shelves

  Module:      shelves (table shelves, IDs like shl_…)
  API:         /v1/shelves, for the signed-in user's shelves
  Permissions: shelves.shelf.read, shelves.shelf.write (the user role)
  Fields:
    name                 string, 1 to 100 characters, unique
    description          text, up to 2000 characters
    visibility           one of private, shared (default private)
  Files:
    create internal/modules/shelves/module.go
    create internal/modules/shelves/shelves_test.go
    create internal/modules/shelves/domain/shelf.go
    …
    create db/migrations/20260920000002_shelves.sql
    modify internal/modules/modules.gen.go
```

`--plural Shelves`, because the plural of shelf isn't shelfs. `--diff` prints every file as a diff, and the Dev Portal's generators hub shows the same plan before Apply. Run it again without `--dry-run`:

```text
internal/modules/shelves/
├── module.go
├── shelves_test.go
├── domain/        shelf.go, errors.go, shelf_test.go
├── usecase/       service.go, ports.go, create_shelf.go, get_shelf.go, list_shelves.go, update_shelf.go, delete_shelf.go
├── repository/    store.go, insert_shelf.go, select_shelf.go, select_shelves.go, update_shelf.go, delete_shelf.go
└── delivery/      routes.go, responses.go, create_shelf.go, get_shelf.go, list_shelves.go, update_shelf.go, delete_shelf.go
```

The module list now names it, so `main.go` serves it without a change:

<!-- include examples/apps/shelfie/internal/modules/modules.gen.go -->

## What it wrote

The module declares its errors and permissions, and wires the layers:

<!-- include examples/apps/shelfie/internal/modules/shelves/module.go -->

The route table is the books module's, with a cursor-paginated list and an update that needs the `version` the client read:

<!-- include examples/apps/shelfie/internal/modules/shelves/delivery/routes.go -->

The table, with a constraint per rule and an index per sort:

<!-- include examples/apps/shelfie/db/migrations/20260920000002_shelves.sql -->

Migrate and test:

```bash
go run ./cmd/api migrate
go run ./cmd/api openapi --dir api
go test ./internal/modules/shelves/...
```

`shelves_test.go` already checks what matters for a reader's data, through the whole app on a database per test ([chapter 4](04-tests.md)): `TestShelvesAreProtected` sends a request without sign-in (401), another reader's requests for Ada's shelf (404 on get, update and delete, and an empty list) and an API key limited to reading (403 on every change); `TestUpdateAndDeleteShelf` sends a stale version (409 `shelf_version_conflict`) and reads the audit trail.

The code is Shelfie's now: rename a detail, add a rule to `domain/shelf.go`, or add an operation the way [Generating code](../../guides/generating-code.md#adding-an-operation-by-hand) shows.

## Every route, from the terminal

```bash
orb routes --app
```

```text
METHOD  PATH                    OPERATION                              MODULE      GUARDS                                                                  HANDLER           SOURCE
GET     /v1/books               books-get-v1-books                     books       authenticated, permission:books.book.read                               h.listBooks       internal/modules/books/delivery/routes.go:31
POST    /v1/books               books-post-v1-books                    books       authenticated, permission:books.book.write, rate_limit:30/1m0s          h.createBook      internal/modules/books/delivery/routes.go:28
GET     /v1/books/{id}          books-get-v1-books-by-id               books       authenticated, permission:books.book.read                               h.getBook         internal/modules/books/delivery/routes.go:33
PATCH   /v1/books/{id}          books-patch-v1-books-by-id             books       authenticated, permission:books.book.write                              h.updateBook      internal/modules/books/delivery/routes.go:35
DELETE  /v1/books/{id}          books-delete-v1-books-by-id            books       authenticated, permission:books.book.write                              h.deleteBook      internal/modules/books/delivery/routes.go:37
PUT     /v1/phone               phonelogin-put-v1-phone                phonelogin  authenticated, permission:phonelogin.phone.write, rate_limit:5/1h0m0s   h.setPhone        internal/modules/phonelogin/delivery/routes.go:29
POST    /v1/phone-sign-in       phonelogin-post-v1-phone-sign-in       phonelogin  public, rate_limit:20/1h0m0s                                            h.signIn          internal/modules/phonelogin/delivery/routes.go:42
POST    /v1/phone-sign-in/code  phonelogin-post-v1-phone-sign-in-code  phonelogin  public, rate_limit:10/1h0m0s                                            h.sendSignInCode  internal/modules/phonelogin/delivery/routes.go:37
POST    /v1/phone/confirm       phonelogin-post-v1-phone-confirm       phonelogin  authenticated, permission:phonelogin.phone.write, rate_limit:10/1h0m0s  h.confirmPhone    internal/modules/phonelogin/delivery/routes.go:32
GET     /v1/profile             profiles-get-v1-profile                profiles    authenticated, permission:profiles.profile.read                         h.getProfile      internal/modules/profiles/delivery/routes.go:20
PUT     /v1/profile             profiles-put-v1-profile                profiles    authenticated, permission:profiles.profile.write                        h.updateProfile   internal/modules/profiles/delivery/routes.go:24
GET     /v1/shelves             shelves-list                           shelves     authenticated, permission:shelves.shelf.read                            h.listShelves     internal/modules/shelves/delivery/routes.go:30
POST    /v1/shelves             shelves-create                         shelves     authenticated, permission:shelves.shelf.write                           h.createShelf     internal/modules/shelves/delivery/routes.go:26
GET     /v1/shelves/{id}        shelves-get                            shelves     authenticated, permission:shelves.shelf.read                            h.getShelf        internal/modules/shelves/delivery/routes.go:35
PATCH   /v1/shelves/{id}        shelves-update                         shelves     authenticated, permission:shelves.shelf.write                           h.updateShelf     internal/modules/shelves/delivery/routes.go:39
DELETE  /v1/shelves/{id}        shelves-delete                         shelves     authenticated, permission:shelves.shelf.write                           h.deleteShelf     internal/modules/shelves/delivery/routes.go:44

16 routes, 2 public
```

`--app` keeps the routes in Shelfie's source. Without it, `orb routes` lists the library modules' routes too, with no source: sign-in's `/v1/auth/…`, `/ops/…`, `/v1/flags` and `/version`, 143 in all. Shelfie's only public routes are phone sign-in's two ([chapter 7](07-phone-code-sign-in.md)), which say `guard.Public()`: deny by default holds for everything else Shelfie wrote. `orb routes --public` lists what needs no sign-in (the sign-in routes, phone sign-in's and `/version`), `--module shelves` one module, and `--json` is for scripts and CI (for example, failing a build when a new public route appears). The Dev Portal's Routes screen shows the same guards and sources, and opens a source in your editor.

## Middleware and guards

The mobile app sends `X-App-Version`, and old versions should be told to update before they reach the books routes:

```bash
orb gen middleware RequireClientVersion --module books
```

It writes `internal/modules/books/delivery/require_client_version.go`, a `func(http.Handler) http.Handler` whose rule, `checkRequireClientVersion`, lets every request through until you write it, and a table-driven test, then says where it goes: `gorbital.Use(RequireClientVersion)` on the books group in `routes.go`. A rule that needs the signed-in reader, such as an active subscription, is a guard:

```bash
orb gen middleware ActiveSubscription --module books --guard
```

That writes `ActiveSubscription()` (`guard.New`, named `active_subscription`), the error it refuses with, a test that mounts it, and the line mapping the error in `module.go`. Chapter 3 writes both rules by hand; the generator is the same shape with the rule left to you. `orb gen middleware … --global` writes middleware for every route, added in `main.go` with `gorbital.WithMiddleware`.

## Checking the app

```bash
orb doctor --fast
```

```text
  ok    modules        internal/modules/modules.gen.go lists 3 modules: books, profiles, shelves
  ok    stack          the default middleware stack
  ok    timeout        30s, the default (APP_REQUEST_TIMEOUT)
```

`orb doctor` fails when a module directory isn't in `modules.gen.go` (a build outside `orb dev` wouldn't serve it), warns when a custom `gorbital.WithStack` leaves out `Recover` or `Auth`, and reports pending migrations. `phonelogin` isn't listed and isn't reported: its `Module` takes the authenticator, so `main.go` adds it itself ([chapter 7](07-phone-code-sign-in.md)).

## Next

[10. Hardening and partners](../../guides/security-layers.md): timeouts, IP allow lists and verified webhooks (chapter in progress).
