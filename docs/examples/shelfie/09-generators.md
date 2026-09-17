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

> [!NOTE]
> A generated migration's version is the time you run the command, and always after the app's newest one, so an existing database has nothing to apply out of order. The file in `examples/apps/shelfie` carries an earlier version than the chapters before this one, because the app was built in a different order from the one you read it in; yours will be the newest.

Migrate and test:

```bash
go run ./cmd/api migrate
go run ./cmd/api openapi --dir api
go test ./internal/modules/shelves/...
```

`shelves_test.go` already checks what matters for a reader's data, through the whole app on a database per test ([chapter 4](04-tests.md)): `TestShelvesAreProtected` sends a request without sign-in (401), another reader's requests for Ada's shelf (404 on get, update and delete, and an empty list) and an API key limited to reading (403 on every change); `TestUpdateAndDeleteShelf` sends a stale version (409 `shelf_version_conflict`) and reads the audit trail.

With `--org`, the same command writes a module whose records belong to an organisation, under `/v1/orgs/{orgId}/…` with `guard.OrgMember`: [chapter 8](08-book-clubs.md)'s club books are that module, unchanged.

The code is Shelfie's now: rename a detail, add a rule to `domain/shelf.go`, or add an operation the way [Generating code](../../guides/generating-code.md#adding-an-operation-by-hand) shows.

## Every route, from the terminal

```bash
orb routes --app
```

```text
METHOD  PATH                              OPERATION                                     MODULE      GUARDS                                                                  MIDDLEWARE            HANDLER           SOURCE
GET     /v1/books                         books-get-v1-books                            books       authenticated, permission:books.book.read                               RequireClientVersion  h.listBooks       internal/modules/books/delivery/routes.go:35
POST    /v1/books                         books-post-v1-books                           books       authenticated, permission:books.book.write, rate_limit:30/1m0s          RequireClientVersion  h.createBook      internal/modules/books/delivery/routes.go:32
DELETE  /v1/books                         books-delete-v1-books                         books       authenticated, permission:books.book.write, recent_reauth               RequireClientVersion  h.emptyShelf      internal/modules/books/delivery/routes.go:47
GET     /v1/books/export                  books-get-v1-books-export                     books       authenticated, permission:books.book.read, active_subscription          RequireClientVersion  h.exportBooks     internal/modules/books/delivery/routes.go:54
GET     /v1/books/{id}                    books-get-v1-books-by-id                      books       authenticated, permission:books.book.read                               RequireClientVersion  h.getBook         internal/modules/books/delivery/routes.go:37
PATCH   /v1/books/{id}                    books-patch-v1-books-by-id                    books       authenticated, permission:books.book.write                              RequireClientVersion  h.updateBook      internal/modules/books/delivery/routes.go:39
DELETE  /v1/books/{id}                    books-delete-v1-books-by-id                   books       authenticated, permission:books.book.write                              RequireClientVersion  h.deleteBook      internal/modules/books/delivery/routes.go:41
GET     /v1/orgs/{orgId}/club-books       clubbooks-list                                clubbooks   authenticated, org_member:clubbooks.club_book.read                      -                     h.listClubBooks   internal/modules/clubbooks/delivery/routes.go:32
POST    /v1/orgs/{orgId}/club-books       clubbooks-create                              clubbooks   authenticated, org_member:clubbooks.club_book.write                     -                     h.createClubBook  internal/modules/clubbooks/delivery/routes.go:28
GET     /v1/orgs/{orgId}/club-books/{id}  clubbooks-get                                 clubbooks   authenticated, org_member:clubbooks.club_book.read                      -                     h.getClubBook     internal/modules/clubbooks/delivery/routes.go:37
PATCH   /v1/orgs/{orgId}/club-books/{id}  clubbooks-update                              clubbooks   authenticated, org_member:clubbooks.club_book.write                     -                     h.updateClubBook  internal/modules/clubbooks/delivery/routes.go:41
DELETE  /v1/orgs/{orgId}/club-books/{id}  clubbooks-delete                              clubbooks   authenticated, org_member:clubbooks.club_book.write                     -                     h.deleteClubBook  internal/modules/clubbooks/delivery/routes.go:46
PUT     /v1/phone                         phonelogin-put-v1-phone                       phonelogin  authenticated, permission:phonelogin.phone.write, rate_limit:5/1h0m0s   -                     h.setPhone        internal/modules/phonelogin/delivery/routes.go:29
POST    /v1/phone-sign-in                 phonelogin-post-v1-phone-sign-in              phonelogin  public, rate_limit:20/1h0m0s                                            -                     h.signIn          internal/modules/phonelogin/delivery/routes.go:42
POST    /v1/phone-sign-in/code            phonelogin-post-v1-phone-sign-in-code         phonelogin  public, rate_limit:10/1h0m0s                                            -                     h.sendSignInCode  internal/modules/phonelogin/delivery/routes.go:37
POST    /v1/phone/confirm                 phonelogin-post-v1-phone-confirm              phonelogin  authenticated, permission:phonelogin.phone.write, rate_limit:10/1h0m0s  -                     h.confirmPhone    internal/modules/phonelogin/delivery/routes.go:32
GET     /v1/profile                       profiles-get-v1-profile                       profiles    authenticated, permission:profiles.profile.read                         -                     h.getProfile      internal/modules/profiles/delivery/routes.go:20
PUT     /v1/profile                       profiles-put-v1-profile                       profiles    authenticated, permission:profiles.profile.write                        -                     h.updateProfile   internal/modules/profiles/delivery/routes.go:24
GET     /v1/purchases                     partners-get-v1-purchases                     partners    authenticated, permission:partners.purchase.read                        -                     h.listPurchases   internal/modules/partners/delivery/routes.go:40
GET     /v1/shelves                       shelves-list                                  shelves     authenticated, permission:shelves.shelf.read                            -                     h.listShelves     internal/modules/shelves/delivery/routes.go:30
POST    /v1/shelves                       shelves-create                                shelves     authenticated, permission:shelves.shelf.write                           -                     h.createShelf     internal/modules/shelves/delivery/routes.go:26
GET     /v1/shelves/{id}                  shelves-get                                   shelves     authenticated, permission:shelves.shelf.read                            -                     h.getShelf        internal/modules/shelves/delivery/routes.go:35
PATCH   /v1/shelves/{id}                  shelves-update                                shelves     authenticated, permission:shelves.shelf.write                           -                     h.updateShelf     internal/modules/shelves/delivery/routes.go:39
DELETE  /v1/shelves/{id}                  shelves-delete                                shelves     authenticated, permission:shelves.shelf.write                           -                     h.deleteShelf     internal/modules/shelves/delivery/routes.go:44
POST    /v1/webhooks/partners/purchases   partners-post-v1-webhooks-partners-purchases  partners    public, webhook, rate_limit:600/1m0s                                    -                     h.purchase        internal/modules/partners/delivery/routes.go:33

25 routes, 3 public
```

That is the finished app in `examples/apps/shelfie`, so it already has the two partner routes [chapter 10](10-hardening.md) adds; at the end of this chapter the listing is 23 routes. The `MIDDLEWARE` column is [chapter 3](03-your-own-middleware.md)'s `RequireClientVersion` on the books group.

`--app` keeps the routes in Shelfie's source. Without it, `orb routes` lists the library modules' routes too, with no source: sign-in's `/v1/auth/…`, `/ops/…`, `/v1/flags`, the organisations module's `/v1/orgs/…` and `/v1/invitations/…`, and `/version`, 181 in all, of which the 25 above have a source. Shelfie's only public routes are phone sign-in's two ([chapter 7](07-phone-code-sign-in.md)) and the partner webhook ([chapter 10](10-hardening.md)), which say `guard.Public()`: deny by default holds for everything else Shelfie wrote. `orb routes --public` lists what needs no sign-in (the sign-in routes, those three and `/version`), `--module shelves` one module, and `--json` is for scripts and CI (for example, failing a build when a new public route appears). The Dev Portal's Routes screen shows the same guards and sources, and opens a source in your editor.

## Middleware and guards

The mobile app sends `X-App-Version`, and old versions should be told to update before they reach the books routes:

```bash
orb gen middleware RequireClientVersion --module books
```

It writes `internal/modules/books/delivery/require_client_version.go`, a `func(http.Handler) http.Handler` whose rule, `checkRequireClientVersion`, lets every request through until you write it, and a table-driven test, then says where it goes: `gorbital.Use(RequireClientVersion)` on the books group in `routes.go`. A rule that needs the signed-in reader, such as an active subscription, is a guard:

```bash
orb gen middleware ActiveSubscription --module books --guard
```

That writes `ActiveSubscription()` (`guard.New`, named `active_subscription`), the error it refuses with, a test that mounts it, and the line mapping the error in `module.go`. [Chapter 3](03-your-own-middleware.md) wrote both of these by hand; the generator is the same shape, with the rule left to you. `orb gen middleware … --global` writes middleware for every route, added in `main.go` with `gorbital.WithMiddleware`.

## Checking the app

```bash
orb doctor --fast
```

```text
  ok    modules        internal/modules/modules.gen.go lists 4 modules: books, clubbooks, profiles, shelves
  ok    stack          the default middleware stack
  ok    timeout        30s, the default (APP_REQUEST_TIMEOUT)
```

`orb doctor` fails when a module directory isn't in `modules.gen.go` (a build outside `orb dev` wouldn't serve it), warns when a custom `gorbital.WithStack` leaves out `Recover` or `Auth`, and reports pending migrations. `phonelogin` isn't listed and isn't reported, and neither is [chapter 10](10-hardening.md)'s `partners`: their `Module` takes an argument, so `orb gen modules` leaves them to `main.go`, which adds them itself, as it adds `orgshttp`.

## Next

[10. Hardening and partners](10-hardening.md): a partner's signed webhooks, timeouts, the `/ops` IP allow list and per-route rate limits.
