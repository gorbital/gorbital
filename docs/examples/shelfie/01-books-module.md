# 1. A books module

A reader's shelf: add a book, list them, change one's status as you read it, remove it. This chapter writes Shelfie's first module, `books`, with its table, its rules, its SQL and its routes. The module has four layers, and each operation is one file in each layer, so a change to "update a book" touches `update_book.go` files and nothing else ([Modules and routes](../../guides/modules-and-routes.md#where-a-module-lives)).

```text
internal/modules/books/
├── module.go
├── domain/        book.go, errors.go
├── usecase/       service.go, ports.go, create_book.go, get_book.go, list_books.go, update_book.go, delete_book.go
├── repository/    store.go, insert_book.go, select_book.go, select_books.go, update_book.go, delete_book.go
└── delivery/      routes.go, responses.go, create_book.go, get_book.go, list_books.go, update_book.go, delete_book.go
```

## The table

`db/migrations/20260920000001_books.sql`:

<!-- include examples/apps/shelfie/db/migrations/20260920000001_books.sql -->

Apply it with `go run ./cmd/api migrate` (`orb dev` does it when the file changes). Each reader's books are theirs: every query filters on `owner_id`.

## The domain

`domain/` holds the book and its rules, in plain Go with no imports beyond the standard library, so they're tested without a database. A new book is validated and normalised in one place:

<!-- include examples/apps/shelfie/internal/modules/books/domain/book.go#new-book -->

The module's errors are sentinel values; the use cases return them, and `module.go` maps each to a status and a stable code:

<!-- include examples/apps/shelfie/internal/modules/books/domain/errors.go#errors -->

## The use cases

`usecase/service.go` holds what every operation shares: the store, the audit log and the clock.

<!-- include examples/apps/shelfie/internal/modules/books/usecase/service.go#service -->

The use cases own the port the repository implements, in `ports.go`:

<!-- include examples/apps/shelfie/internal/modules/books/usecase/ports.go#store -->

And each operation is a file. Adding a book finds the signed-in reader, applies the domain rules, stores the book and records an audit event:

<!-- include examples/apps/shelfie/internal/modules/books/usecase/create_book.go#create-book -->

The use case doesn't check permissions: the route's guard did before the request body was even read. It does check who the reader is, because a book belongs to them.

## The repository

One SQL statement per file, through `postgres.DBTX`, so the same store works on the pool or inside a transaction. Database conditions the use cases handle become domain errors: no row is `ErrBookNotFound`, the unique ISBN per shelf is `ErrISBNTaken`.

<!-- include examples/apps/shelfie/internal/modules/books/repository/insert_book.go#insert-book -->

## The routes

`delivery/routes.go` is the whole route table: every path, its summary, status and guards, in one place.

<!-- include examples/apps/shelfie/internal/modules/books/delivery/routes.go#routes -->

Every route requires a signed-in reader, because none is `guard.Public()`; `guard.Permission` then requires the permission, and `guard.RateLimit` limits how fast a reader adds books. [Chapter 2](02-protecting-routes.md) goes through each guard and what a refused client is told; the last two routes, the module's own `RequireClientVersion` middleware and its `ActiveSubscription()` guard are [chapter 3](03-your-own-middleware.md)'s.

Each operation's file holds its input and output types and its handler. The struct tags are the API's validation and its OpenAPI document:

<!-- include examples/apps/shelfie/internal/modules/books/delivery/create_book.go#create-book -->

## The module

`module.go` names the module, maps its errors, declares its permissions and wires the layers:

<!-- include examples/apps/shelfie/internal/modules/books/module.go#module -->

`Routes` is also called with zero `Deps` when `go run ./cmd/api openapi` exports the document without a database: the service is built on a nil pool, and no handler runs.

Permissions name the roles that hold them: every signed-in user holds `user`, so every reader can manage their own shelf, while an API key holds them only when its scopes include them.

## Add it to the app

```bash
orb gen modules
```

```text
✓ Wrote internal/modules/modules.gen.go: 1 module: books
```

With `orb dev` running, this happens on save. Restart, and `/docs` lists the book routes, each with its bearer requirement, its 401 and 403 responses, and the guards in `x-gorbital-guards`. Write the OpenAPI document for clients:

```bash
go run ./cmd/api openapi --dir api
```

## Try it

Without signing in, a request answers 401:

```bash
curl -i -X POST http://127.0.0.1:8080/v1/books -H 'Content-Type: application/json' -d '{"title": "Dune"}'
```

```json
{"title":"Unauthorized","status":401,"code":"unauthenticated","detail":"authentication is required","request_id":"req_…"}
```

Deny by default works before there is anyone to sign in.

## Next

[2. Protecting routes](02-protecting-routes.md): what every guard in that route table does, and what a client is told when one refuses.
