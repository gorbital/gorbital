# 1. A books module

A reader's shelf: add a book, list them, change one's status as you read it, remove it. This chapter adds Shelfie's first module, `books`, with its table, its rules, its SQL and its routes. The module has four layers, and each operation is one file in each layer, so a change to "update a book" touches `update_book.go` files and nothing else ([Modules and routes](../../guides/modules-and-routes.md#where-a-module-lives)).

```text
internal/modules/books/
├── module.go
├── domain/        book.go, errors.go
├── usecase/       service.go, ports.go, create_book.go, get_book.go, list_books.go, update_book.go, delete_book.go
├── repository/    store.go, insert_book.go, select_book.go, select_books.go, update_book.go, delete_book.go
└── delivery/      routes.go, responses.go, create_book.go, get_book.go, list_books.go, update_book.go, delete_book.go
```

That is twenty-three files for five routes, and writing them out by hand is not the point of the chapter. `orb gen module` writes all of them, and its migration, in this layout; the rest of the chapter reads the ones worth reading.

## Generate it

In the app from [chapter 0](00-start-a-project.md), with a clean git tree:

```bash
orb gen module Book title:string author:string isbn:string:unique 'status:enum(want_to_read,reading,read)'
```

```text
✓ Created module books

  Module:      books (table books, IDs like bok_…)
  API:         /v1/books, for the signed-in user's books
  Permissions: books.book.read, books.book.write (the user role)
  Fields:
    title                string, 1 to 100 characters
    author               string, 1 to 100 characters
    isbn                 string, 1 to 100 characters, unique
    status               one of want_to_read, reading, read (default want_to_read)
  Files:
    create internal/modules/books/module.go
    create internal/modules/books/books_test.go
    create internal/modules/books/domain/book.go
    …
    create internal/modules/books/delivery/delete_book.go
    create db/migrations/20260918000071_books.sql
    modify internal/modules/modules.gen.go

Next:
  1. go run ./cmd/api migrate (orb dev runs it)
  2. go run ./cmd/api openapi --dir api
  3. go test ./internal/modules -run TestPublicSurface -update (records the new error codes, audit actions and permissions in api/surface.json)
  4. go test ./internal/modules/books/...
  5. go run ./cmd/api, sign in, then POST /v1/books
```

It also writes the module's tests, adds the module to `internal/modules/modules.gen.go`, and stops there: the code is yours from now on. [Chapter 9](09-generators.md) is about the generator itself — its fields, its flags and what else it writes.

The rest of this chapter reads Shelfie's own books module, which is that skeleton with Shelfie's rules in it: an author is optional, an ISBN is optional and normalised, and a listing is the newest twenty books rather than the generator's sortable, cursor-paginated one. Where a file already carries work from a later chapter, the text says so, so you know what you're looking at. The whole module is in [`examples/shelfie/internal/modules/books`](https://github.com/gorbital/gorbital/tree/main/examples/shelfie/internal/modules/books).

## The table

The generator wrote `db/migrations/<timestamp>_books.sql`. Shelfie's version, which the app is built on, makes the author optional and the ISBN both optional and checked:

<!-- include examples/shelfie/db/migrations/20260920000001_books.sql -->

Apply it with `go run ./cmd/api migrate` (`orb dev` does it when the file changes). A migration is yours to change until it is released; afterwards, add a new one. Each reader's books are theirs: every query filters on `owner_id`.

## The domain

`domain/` holds the book and its rules, in plain Go with no imports beyond the standard library, so they're tested without a database. A new book is validated and normalised in one place:

<!-- include examples/shelfie/internal/modules/books/domain/book.go#new-book -->

The module's errors are sentinel values; the use cases return them, and `module.go` maps each to a status and a stable code:

<!-- include examples/shelfie/internal/modules/books/domain/errors.go#errors -->

The last of them, `ErrShelfFull`, belongs to the shelf limit in [chapter 5](05-operations.md); the other seven are this chapter's.

## The use cases

`usecase/service.go` holds what every operation shares: the store, the audit log, the logger and the clock.

<!-- include examples/shelfie/internal/modules/books/usecase/service.go#service -->

`shelfLimit` is [chapter 5](05-operations.md)'s: a runtime setting operators change through `/ops`, read on every new book. A module without a limit passes nil for it, and nothing is checked.

The use cases own the port the repository implements, in `ports.go`:

<!-- include examples/shelfie/internal/modules/books/usecase/ports.go#store -->

Five of those seven methods are this chapter's five operations. `DeleteBooks` belongs to "empty my shelf" ([chapter 2](02-protecting-routes.md)) and `CountBooks` to the shelf limit ([chapter 5](05-operations.md)). One repository file implements one method, so adding a method here means adding a file under `repository/`.

And each operation is a file. Adding a book finds the signed-in reader, applies the domain rules, stores the book and records an audit event:

<!-- include examples/shelfie/internal/modules/books/usecase/create_book.go#create-book -->

The use case doesn't check permissions: the route's guard did before the request body was even read. It does check who the reader is, because a book belongs to them.

## The repository

One SQL statement per file, through `postgres.DBTX`, so the same store works on the pool or inside a transaction. Database conditions the use cases handle become domain errors: no row is `ErrBookNotFound`, the unique ISBN per shelf is `ErrISBNTaken`.

<!-- include examples/shelfie/internal/modules/books/repository/insert_book.go#insert-book -->

`store.go` holds what the files share: the column list, the row scanner, and the `driverError` that turns "no rows" and the `books_owner_isbn` unique violation into those two domain errors. Anything else stays a driver error and never reaches the API.

## The routes

`delivery/routes.go` is the whole route table: every path, its summary, status and guards, in one place.

<!-- include examples/shelfie/internal/modules/books/delivery/routes.go#routes -->

Three things here arrive later and are worth skipping for now: `DELETE /v1/books` and its `guard.RecentReauth()` are [chapter 2](02-protecting-routes.md), and both the `RequireClientVersion` middleware on the group and `GET /v1/books/export`, with the `subs` argument and the `ActiveSubscription` guard, are [chapter 3](03-your-own-middleware.md). What the generator wrote, and what this chapter is about, is the five routes in the middle.

Every route requires a signed-in reader, because none is `guard.Public()`; `guard.Permission` then requires the permission, and `guard.RateLimit` limits how fast a reader adds books ([Guards and middleware](../../guides/guards-and-middleware.md) covers every guard).

Each operation's file holds its input and output types and its handler. The struct tags are the API's validation and its OpenAPI document:

<!-- include examples/shelfie/internal/modules/books/delivery/create_book.go#create-book -->

## The module

`module.go` names the module, maps its errors, declares its permissions and wires the layers:

<!-- include examples/shelfie/internal/modules/books/module.go#module -->

Again, later chapters are visible in it: the `active_subscription_refused` mapping is [chapter 3](03-your-own-middleware.md), and the `shelf_full` mapping, the `Settings` and `Flags` functions and the `shelfLimit` argument to `NewService` are [chapter 5](05-operations.md). What the generator writes is the name, the mappings for its own errors, the two permissions and `Routes`.

`Routes` is also called with zero `Deps` when `go run ./cmd/api openapi` exports the document without a database: the service is built on a nil pool, and no handler runs.

Permissions name the roles that hold them: every signed-in user holds `user`, so every reader can manage their own shelf, while an API key holds them only when its scopes include them.

## Add it to the app

The generator already added `books` to `internal/modules/modules.gen.go`. Now that `db/migrations` has a migration of Shelfie's own, the two sample modules from chapter 0 can go, along with the test that checks them:

```bash
rm -r internal/modules/ping internal/modules/projects db/migrations/*_projects.sql cmd/api/app_test.go
orb gen modules
```

```text
✓ Wrote internal/modules/modules.gen.go: 1 module: books
```

With `orb dev` running, this happens on save. Then apply the migration, record the module's public names, and write the OpenAPI document for clients:

```bash
go run ./cmd/api migrate
go test ./internal/modules -run TestPublicSurface -update
go run ./cmd/api openapi --dir api
```

`TestPublicSurface` freezes the error codes, audit actions, permissions, settings and flags an app exposes, in `api/surface.json`, so none of them changes by accident. It fails until `-update` records what changed: here, three new error codes, three new audit actions and two new permissions from the books module, and the sample modules' names going away. Read the diff before committing it: names that leave the file are a breaking change for clients that handle them, which is exactly why the test asks.

Restart, and `/docs` lists the five book routes, each with its bearer requirement, its 401 and 403 responses, and the guards in `x-gorbital-guards`.

## Try it

Without signing in, a request answers 401:

```bash
curl -X POST http://127.0.0.1:8080/v1/books -H 'Content-Type: application/json' -d '{"title": "Dune"}'
```

```json
{"title":"Unauthorized","status":401,"code":"unauthenticated","detail":"authentication is required","request_id":"req_…"}
```

Deny by default works before there is anyone to sign in. The tests in [chapter 4](04-tests.md) call the routes as a signed-in reader.

## Next

[2. Protecting routes](02-protecting-routes.md): the guards on those routes, and what each one refuses.
