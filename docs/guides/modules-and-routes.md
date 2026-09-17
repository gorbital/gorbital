# Modules and routes

A **module** is one feature of your app, declared in one value: its routes, the errors it returns, the permissions it checks, and its runtime settings and feature flags. The package is `gorbital.dev/gorbital` ([Methods](../methods/gorbital.md)); the decisions are [ADR-0082](../adr/0082-routes-guards-and-middleware.md) and [ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md).

> **Status: v0.2, in progress.** Modules and routes are in the library now. `gorbital.Main`, which builds the whole app from a list of modules, arrives in Phase 3 of the [v0.2 roadmap](../v0.2-roadmap.md). Until then, a v0.1 app adds modules to its existing `internal/app` with `Declare` and `Mount`, as shown [below](#using-modules-in-a-v01-app).

## A module

```go
package books

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/settings"
)

// Permissions and error codes are public API: add new ones, never rename them.
const (
	PermRead  = "books.book.read"
	PermWrite = "books.book.write"
)

func Module() gorbital.Module {
	var pageSize *settings.Setting[int]
	return gorbital.Module{
		Name: "books",
		Errors: []httpx.Mapping{
			{Err: ErrBookNotFound, Status: http.StatusNotFound, Code: "book_not_found", Detail: "no book of yours has this ID"},
			{Err: ErrISBNTaken, Status: http.StatusConflict, Code: "isbn_taken", Detail: "you already have a book with this ISBN"},
		},
		Permissions: []gorbital.Permission{
			{Name: PermRead, Description: "Read your books", Roles: []string{"user"}},
			{Name: PermWrite, Description: "Add, change and remove your books", Roles: []string{"user"}},
		},
		Settings: func(r *settings.Registry) {
			pageSize = settings.Int(r, "books.page_size", 20, settings.Range(1, 100))
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			h := &handler{db: d.DB, audit: d.Audit, pageSize: pageSize}
			books := r.Group("/v1/books", gorbital.Tags("Books"))
			gorbital.Get(books, "", h.listBooks)
			gorbital.Post(books, "", h.createBook, gorbital.Status(http.StatusCreated), gorbital.Errors(http.StatusConflict))
			gorbital.Get(books, "/{id}", h.getBook, gorbital.Errors(http.StatusNotFound))
			gorbital.Delete(books, "/{id}", h.deleteBook, gorbital.Status(http.StatusNoContent))
		},
	}
}
```

| Field | What it holds | When it runs |
|---|---|---|
| `Name` | Lowercase snake_case, unique in the app. Used in operation IDs, errors and the module's logger | — |
| `Routes` | The module's operations | Once, by `Mount`, after the stores exist |
| `Errors` | Mappings from your errors to problem responses | Added by `Mount` |
| `Permissions` | Permissions the module checks and the roles that hold them | Declared by `Declare` |
| `Settings`, `Flags` | Runtime settings and feature flags | Declared by `Declare`, before their stores are built |

Keep what `Settings` and `Flags` return in variables of the `Module` function, as `pageSize` above, and pass them to your handlers. There's no lookup by name.

## Routes

A route is one call: `gorbital.Get`, `Post`, `Put`, `Patch` or `Delete`, with a router, a path and your handler.

```go
func (h *handler) getBook(ctx context.Context, in *bookIDInput) (*bookOutput, error)
```

The handler takes the request context and a pointer to its **input** struct, and returns a pointer to its **output** struct or an error. The structs are [Huma](https://huma.rocks)'s: `path`, `query` and `header` tags, a `Body` field, and validation tags such as `maxLength`. Huma validates the input and writes the OpenAPI document from both types, so the docs at `/docs` always match the code.

The verbs are functions, not methods on the router, because Go methods can't be generic: `gorbital.Get(books, "/{id}", h.getBook)`.

### Groups

`r.Group(prefix, options...)` returns a router for routes under a prefix. Groups nest, and a group's options apply to every route in it; a route's own options come after and win.

```go
v1 := r.Group("/v1", gorbital.Tags("Books"))
books := v1.Group("/books")
gorbital.Get(books, "/{id}", h.getBook)   // GET /v1/books/{id}, tagged Books
```

Paths and prefixes are empty or start with a slash, and never end with one.

### Route options

| Option | Sets | Without it |
|---|---|---|
| `Summary("Get a book")` | The OpenAPI summary | Generated from method and path: "Get v1 books by ID" |
| `Description(markdown)` | The OpenAPI description | None |
| `Tags("Books")` | OpenAPI tags, which group operations in `/docs` | None |
| `OperationID("books-get")` | The operation ID | `<module>-` plus one generated from method and path: `books-get-v1-books-by-id` |
| `Status(http.StatusCreated)` | The success status | 200, or 204 for an output without a body |
| `Errors(http.StatusNotFound)` | Error statuses to document | Only those guards document |
| `Deprecated()` | Marks the operation deprecated | — |
| `guard.Public()` | See below | The route requires sign-in |

**Operation IDs are public API**: client generators name functions after them. Set `OperationID` on routes you publish, so moving a route doesn't rename a client's function.

## Every route requires sign-in unless it's public

A route without `guard.Public()` answers **401 `unauthenticated`** to a request without an authenticated user, service account or API key. The check runs **before the request body is read**, and the OpenAPI document shows the route's bearer requirement and its 401 response.

```go
gorbital.Get(books, "/{id}", h.getBook)                                     // signed-in callers only
gorbital.Get(r.Group("/v1/catalog", guard.Public()), "/{id}", h.catalogBook) // anyone
```

Forgetting a guard can't expose a route; forgetting `guard.Public()` gives a 401 that you'll notice at once. Permissions, rate limits, re-authentication, your own guards and middleware are in [Guards and middleware](guards-and-middleware.md).

## Errors in routes

Return your module's errors from handlers; `Errors` maps them to problem responses with a stable `code`:

```json
{ "title": "Not Found", "status": 404, "code": "book_not_found", "detail": "no book of yours has this ID", "request_id": "req_…" }
```

An error nobody mapped is a 500 with a generic detail, logged once with the request ID. [Error handling](error-handling.md) has the full contract.

## What `Declare` and `Mount` refuse

Both return an error naming the module, so the app stops at start instead of misbehaving:

| Mistake | Error |
|---|---|
| Two modules with one name, or a name that isn't snake_case | `gorbital: two modules are named "books"` |
| The same method and path in two modules (parameter names don't matter) | `gorbital: GET /v1/books/{bookID} is registered by modules "books" and "shelves"` |
| The same operation ID twice | `gorbital: operation ID "get" is used by modules "books" and "shelves"` |
| A permission declared by two modules | `gorbital: permission "books.book.read" is declared by modules "books" and "shelves"` |
| A path without a leading slash, with a trailing slash or an empty segment | `gorbital: module "books": GET "v1/books": "v1/books" must start with a slash` |
| A protected route on an API without the bearer scheme | `… requires authentication, but the API declares no bearer security scheme` |
| A setting or flag the registry refuses (invalid or duplicate key) | `gorbital: module "shelves": settings: settings: shared.limit: declared twice` |
| An input or output type Huma can't document | `gorbital: module "books": POST /v1/books: …` |

## Using modules in a v0.1 app

Until `gorbital.Main` arrives, add modules to the app you have. Three places in `internal/app` change.

**1. Declare** permissions, settings and flags where the registries are built: `permissions.go` and `app.go`.

```go
// permissions.go
var appModules = []gorbital.Module{books.Module()}

func declarePermissions() *authlib.Catalog {
	c := authlib.NewCatalog()
	// … the existing c.Permission calls …
	user := []string{flagsusecase.PermFlagsRead}
	// … the existing user resource permissions …

	if err := gorbital.Declare(gorbital.Declarations{Permissions: c}, appModules...); err != nil {
		panic(err) // a programming error, like the catalog's own panics
	}
	user = append(user, gorbital.Grants(authusecase.RoleUser, appModules...)...)
	c.Role(authusecase.RoleUser, "Held by every signed-in user without a grant; by API keys only within their scopes", user...)
	// … the other roles …
}
```

With `Roles: []string{"user"}` on a module's permission, `Grants` returns it for the user role that every signed-in user holds.

```go
// app.go, before settings.NewStore and flags.NewStore
if err := gorbital.Declare(gorbital.Declarations{Settings: reg, Flags: flagReg}, appModules...); err != nil {
	return err
}
```

`Declare` accepts each registry separately, so it can be called once per place the registries are built. A module's permissions are declared once, where the catalog is built.

**2. Mount** the modules in `routes.go`, after `registerModules`:

```go
if err := gorbital.Mount(api, mapper, gorbital.Deps{
	DB: svc.db, Audit: svc.recorder, Mailer: a.mailer, Jobs: a.jobs,
	Settings: a.settings, Flags: a.flags, Storage: a.storage, Logger: a.logger,
}, appModules...); err != nil {
	return err
}
```

When the app exports its OpenAPI document without a database, pass `gorbital.Deps{}`: modules register the same routes either way.

**3. Nothing else.** The authentication middleware in `routes.go` already sets the actor that the sign-in check reads, and `openapi.InstallErrors(mapper)` already writes mapped errors as problem+json.

## Testing a module

Mount the module on a test API and send requests through it. Set the actor the way the authentication middleware does:

```go
func TestGetBookRequiresSignIn(t *testing.T) {
	mux := http.NewServeMux()
	api := openapi.New(mux, "test", "1.0.0", openapi.WithBearerAuth("session"))
	mapper, _ := httpx.NewMapper(slog.New(slog.DiscardHandler))
	openapi.InstallErrors(mapper)
	if err := gorbital.Mount(api, mapper, gorbital.Deps{}, books.Module()); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/books/bok_1", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
```

A test kit that builds the whole app and signs requests in (`gorbitaltest`) arrives in Phase 3.

## Performance

Registering a route through `gorbital` adds the sign-in check to the operation and nothing else. The benchmark `BenchmarkRequest` in `gorbital/bench_test.go` compares a signed-in GET through `gorbital.Get` with the same operation registered directly on Huma: on an Apple M1 Max with Go 1.26.0 (`-count 5`), both took 1.5–1.9 µs and 19 allocations (about 1.66 KiB) per request, with no measurable difference. See [benchmarks](../benchmarks.md) for how budgets are checked.
