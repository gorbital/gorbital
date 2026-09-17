# gorbital

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital"
```

Package gorbital composes gorbital's modules into an application (ADR-0081). A [Module](#Module) declares one feature of an app: its routes, error mappings, permissions, runtime settings and feature flags. [Declare](#Declare) adds the declarations to the app's registries before their stores are built, and [Mount](#Mount) registers the error mappings and routes on the app's API.

Routes are declared with the generic functions [Get](#Get), [Post](#Post), [Put](#Put), [Patch](#Patch) and [Delete](#Delete) on a [Router](#Router), so handlers keep their typed input and output, and with them request validation and the OpenAPI document. Every route requires an authenticated actor unless it has guard.Public() (ADR-0082):

```go
func Module() gorbital.Module {
	return gorbital.Module{
		Name:   "books",
		Errors: []httpx.Mapping{{Err: ErrISBNTaken, Status: http.StatusConflict, Code: "isbn_taken"}},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			h := &handler{db: d.DB, audit: d.Audit}
			books := r.Group("/v1/books", gorbital.Tags("Books"))
			gorbital.Post(books, "", h.createBook, gorbital.Status(http.StatusCreated))
			gorbital.Get(books, "/{id}", h.getBook)
		},
	}
}
```

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Functions: [`Declare`](#Declare), [`Delete`](#Delete), [`Get`](#Get), [`Grants`](#Grants), [`Mount`](#Mount), [`Patch`](#Patch), [`Post`](#Post), [`Put`](#Put)
- Types:
  - [`Declarations`](#Declarations)
  - [`Deps`](#Deps)
  - [`Module`](#Module)
  - [`Permission`](#Permission)
  - [`PermissionDeclarer`](#PermissionDeclarer)
  - [`RouteOption`](#RouteOption): [`Deprecated`](#Deprecated), [`Description`](#Description), [`Errors`](#Errors), [`OperationID`](#OperationID), [`Status`](#Status), [`Summary`](#Summary), [`Tags`](#Tags), [`Use`](#Use)
  - [`Router`](#Router): [`Router.Group`](#Router.Group)

## Functions

<a id="Declare"></a>

### func Declare

```go
func Declare(d Declarations, modules ...Module) error
```

Declare adds the modules' permissions, runtime settings and feature flags to d's registries, in module order. Call it once, before building the settings and flags stores and before freezing the permission catalog; then grant each role its permissions with [Grants](#Grants).

It returns an error naming the module for an invalid or duplicate module name, a permission declared by two modules, a missing registry, or an invalid declaration (which the registries report by panicking).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var pageSize *settings.Setting[int]
books := gorbital.Module{
	Name:        "books",
	Permissions: []gorbital.Permission{{Name: "books.book.read", Description: "Read books", Roles: []string{"user"}}},
	Settings:    func(r *settings.Registry) { pageSize = settings.Int(r, "books.page_size", 20) },
}

catalog := auth.NewCatalog()
reg := settings.NewRegistry()
if err := gorbital.Declare(gorbital.Declarations{Permissions: catalog, Settings: reg}, books); err != nil {
	panic(err)
}
catalog.Role("user", "Every signed-in user", gorbital.Grants("user", books)...)

fmt.Println(reg.Keys(), pageSize != nil)
fmt.Println(catalog.Permissions("user"))
```

Output:

```text
[books.page_size] true
[books.book.read]
```

<a id="Delete"></a>

### func Delete

```go
func Delete[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Delete registers a DELETE operation. See [Get](#Get).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Delete(r, "/v1/books/{id}", removeBook, gorbital.Status(http.StatusNoContent))
}, http.MethodDelete, "/v1/books/{id}")
```

Output:

```text
DELETE /v1/books/{id} id=books-delete-v1-books-by-id summary="Delete v1 books by ID" tags=[] secured=true deprecated=false
```

<a id="Get"></a>

### func Get

```go
func Get[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Get registers a GET operation for path under r's prefix. The handler takes the request context and its typed input, and returns its typed output or an error; Huma validates the input and documents both (ADR-0082).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Summary("Get a book"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[] secured=true deprecated=false
```

<a id="Grants"></a>

### func Grants

```go
func Grants(role string, modules ...Module) []string
```

Grants returns the permissions the modules give to role, sorted and without duplicates, for declaring the role in the permission catalog.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := gorbital.Module{Name: "books", Permissions: []gorbital.Permission{
	{Name: "books.book.read", Roles: []string{"user", "viewer"}},
	{Name: "books.book.write", Roles: []string{"user"}},
}}
shelves := gorbital.Module{Name: "shelves", Permissions: []gorbital.Permission{
	{Name: "shelves.shelf.read", Roles: []string{"viewer"}},
}}
fmt.Println(gorbital.Grants("user", books, shelves))
fmt.Println(gorbital.Grants("viewer", books, shelves))
```

Output:

```text
[books.book.read books.book.write]
[books.book.read shelves.shelf.read]
```

<a id="Mount"></a>

### func Mount

```go
func Mount(api huma.API, mapper *httpx.Mapper, deps Deps, modules ...Module) error
```

Mount adds each module's error mappings to mapper and registers its routes on api, in module order. The app calls it once, after [Declare](#Declare) and after building the stores that deps hold; to export the OpenAPI document without a database, pass zero Deps.

api must declare the bearer security scheme (openapi.WithBearerAuth) when any route requires authentication, and errors are written as problem+json only when the app has called openapi.InstallErrors with mapper.

Mount returns the first error, naming the module: an invalid or duplicate module name, errors to map without a mapper, a mapping the mapper refuses, an invalid path, two routes with the same method and path or the same operation ID, or a route Huma can't register (such as an unsupported input type). Routes registered before the error stay registered, so an app treats any error as fatal.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
mux, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/books/{id}", findBook)                   // signed-in callers only
		gorbital.Get(r, "/v1/catalog/{id}", findBook, guard.Public()) // anyone
	},
})
if err != nil {
	panic(err)
}
fmt.Println(call(mux, http.MethodGet, "/v1/books/bok_1", false))
fmt.Println(call(mux, http.MethodGet, "/v1/books/bok_1", true))
fmt.Println(call(mux, http.MethodGet, "/v1/catalog/bok_1", false))
```

Output:

```text
401 unauthenticated
200
200
```

**Example (duplicateRoute)**

```go
_, api, mapper := newAPI()
route := func(name string) gorbital.Module {
	return gorbital.Module{Name: name, Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.OperationID(name+"-get-book"))
	}}
}
err := gorbital.Mount(api, mapper, gorbital.Deps{}, route("books"), route("library"))
fmt.Println(err)
```

Output:

```text
gorbital: GET /v1/books/{id} is registered by modules "books" and "library"
```

<a id="Patch"></a>

### func Patch

```go
func Patch[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Patch registers a PATCH operation. See [Get](#Get).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Patch(r, "/v1/books/{id}", findBook, gorbital.Summary("Update a book"))
}, http.MethodPatch, "/v1/books/{id}")
```

Output:

```text
PATCH /v1/books/{id} id=books-patch-v1-books-by-id summary="Update a book" tags=[] secured=true deprecated=false
```

<a id="Post"></a>

### func Post

```go
func Post[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Post registers a POST operation. See [Get](#Get).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Post(r, "/v1/books", addBook, gorbital.Status(http.StatusCreated))
}, http.MethodPost, "/v1/books")
```

Output:

```text
POST /v1/books id=books-post-v1-books summary="Post v1 books" tags=[] secured=true deprecated=false
```

<a id="Put"></a>

### func Put

```go
func Put[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption)
```

Put registers a PUT operation. See [Get](#Get).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Put(r, "/v1/books/{id}/title", findBook, gorbital.Summary("Replace a book's title"))
}, http.MethodPut, "/v1/books/{id}/title")
```

Output:

```text
PUT /v1/books/{id}/title id=books-put-v1-books-by-id-title summary="Replace a book's title" tags=[] secured=true deprecated=false
```

## Types

<a id="Declarations"></a>
<a id="Declarations.Permissions"></a>
<a id="Declarations.Settings"></a>
<a id="Declarations.Flags"></a>

### type Declarations

```go
type Declarations struct {
	// Permissions receives every module's permissions; nil skips them.
	Permissions PermissionDeclarer
	// Settings and Flags are required when a module declares settings or
	// flags.
	Settings *settings.Registry
	Flags    *flags.Registry
}
```

Declarations are the registries [Declare](#Declare) adds modules' declarations to.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := gorbital.Module{
	Name:  "books",
	Flags: func(r *flags.Registry) { flags.Bool(r, "books.covers", flags.Describe("Show cover images")) },
}
err := gorbital.Declare(gorbital.Declarations{}, books)
fmt.Println(err)
```

Output:

```text
gorbital: module "books" declares flags, but Declarations.Flags is nil
```

<a id="Deps"></a>
<a id="Deps.DB"></a>
<a id="Deps.Audit"></a>
<a id="Deps.Mailer"></a>
<a id="Deps.Jobs"></a>
<a id="Deps.Settings"></a>
<a id="Deps.Flags"></a>
<a id="Deps.Storage"></a>
<a id="Deps.RateLimits"></a>
<a id="Deps.Logger"></a>

### type Deps

```go
type Deps struct {
	DB       *pgxpool.Pool
	Audit    audit.Recorder
	Mailer   mail.Sender
	Jobs     *jobs.Client
	Settings *settings.Store
	Flags    *flags.Store
	Storage  storage.Store
	// RateLimits shares guard.RateLimit budgets across instances; without
	// it, each instance counts on its own.
	RateLimits *ratelimitpg.Store
	// Logger is tagged with the module's name by [Mount].
	Logger *slog.Logger
}
```

Deps are the app's shared dependencies, passed to each module's Routes.

Every field may be nil: all of them are while the OpenAPI document is exported without a database, and Storage is nil unless the app configures file storage. A module registers the same routes either way and uses its dependencies only when handling requests.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		// d.DB, d.Mailer and the others are nil while the OpenAPI
		// document is exported; use them only in handlers.
		d.Logger.Info("registering routes", "database", d.DB != nil)
		gorbital.Get(r, "/v1/books/{id}", findBook)
	},
}
_, api, mapper := newAPI()
fmt.Println(gorbital.Mount(api, mapper, gorbital.Deps{}, books))
```

Output:

```text
<nil>
```

<a id="Module"></a>
<a id="Module.Name"></a>
<a id="Module.Routes"></a>
<a id="Module.Errors"></a>
<a id="Module.Permissions"></a>
<a id="Module.Settings"></a>
<a id="Module.Flags"></a>
<a id="Module.Middleware"></a>

### type Module

```go
type Module struct {
	// Name identifies the module in operation IDs, errors and logs. It is
	// lowercase snake_case, such as "books", and unique in an app.
	Name string

	// Routes registers the module's operations. [Mount] calls it once.
	Routes func(r *Router, d Deps)

	// Errors map the module's errors to problem responses. Error codes are
	// public API: add new ones, never change existing ones (ADR-0015).
	Errors []httpx.Mapping

	// Permissions are the permissions the module checks and the roles that
	// hold them. Permission names are public API.
	Permissions []Permission

	// Settings declares the module's runtime settings. [Declare] calls it
	// once, before the settings store is built.
	Settings func(r *settings.Registry)

	// Flags declares the module's feature flags. [Declare] calls it once,
	// before the flags store is built.
	Flags func(r *flags.Registry)

	// Middleware runs on every route of the module, before group and route
	// middleware (see [Use]).
	Middleware []func(http.Handler) http.Handler
}
```

A Module is one feature of an app. Its package returns it from a function named Module, so declarations such as settings are created in that function's closure and used by Routes without a lookup by name:

```go
func Module() gorbital.Module {
	var pageSize *settings.Setting[int]
	return gorbital.Module{
		Name:     "books",
		Settings: func(r *settings.Registry) { pageSize = settings.Int(r, "books.page_size", 20) },
		Routes:   func(r *gorbital.Router, d gorbital.Deps) { /* use pageSize */ },
	}
}
```

*Since `v0.2.0 (unreleased)`*

**Example**

```go
books := gorbital.Module{
	Name: "books",
	Errors: []httpx.Mapping{
		{Err: ErrBookNotFound, Status: http.StatusNotFound, Code: "book_not_found", Detail: "no book has this ID"},
	},
	Permissions: []gorbital.Permission{
		{Name: "books.book.read", Description: "Read books", Roles: []string{"user"}},
	},
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r.Group("/v1/books"), "/{id}", findBook)
	},
}

mux, api, mapper := newAPI()
if err := gorbital.Mount(api, mapper, gorbital.Deps{}, books); err != nil {
	panic(err)
}
fmt.Println(call(mux, http.MethodGet, "/v1/books/bok_1", true))
fmt.Println(call(mux, http.MethodGet, "/v1/books/bok_9", true))
```

Output:

```text
200
404 book_not_found
```

<a id="Permission"></a>
<a id="Permission.Name"></a>
<a id="Permission.Description"></a>
<a id="Permission.Roles"></a>

### type Permission

```go
type Permission struct {
	Name        string
	Description string
	// Roles are the roles that hold the permission, such as "user". A role
	// the app doesn't declare grants nothing.
	Roles []string
}
```

A Permission is a permission a module checks, such as "books.book.write".

*Since `v0.2.0 (unreleased)`*

**Example**

```go
write := gorbital.Permission{
	Name:        "books.book.write",
	Description: "Add, change and remove books",
	Roles:       []string{"user"},
}
catalog := auth.NewCatalog()
if err := gorbital.Declare(gorbital.Declarations{Permissions: catalog}, gorbital.Module{Name: "books", Permissions: []gorbital.Permission{write}}); err != nil {
	panic(err)
}
fmt.Println(len(catalog.AllPermissions()), catalog.AllPermissions()[0].Name)
```

Output:

```text
1 books.book.write
```

<a id="PermissionDeclarer"></a>
<a id="PermissionDeclarer.Permission"></a>

### type PermissionDeclarer

```go
type PermissionDeclarer interface {
	Permission(name, description string)
}
```

A PermissionDeclarer declares permissions. \*auth.Catalog implements it.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var declared allowList
books := gorbital.Module{Name: "books", Permissions: []gorbital.Permission{{Name: "books.book.read"}, {Name: "books.book.write"}}}
if err := gorbital.Declare(gorbital.Declarations{Permissions: &declared}, books); err != nil {
	panic(err)
}
fmt.Println(declared)
```

Output:

```text
[books.book.read books.book.write]
```

<a id="RouteOption"></a>

### type RouteOption

```go
type RouteOption = route.Option
```

A RouteOption configures a route, or every route of a group. Options of a group apply first, then the route's own.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// Options on a group apply to its routes first; a route's own options
// come after and win.
routesExample(func(r *gorbital.Router) {
	books := r.Group("/v1/books", gorbital.Tags("Books"), gorbital.Summary("A books operation"))
	gorbital.Get(books, "/{id}", findBook, gorbital.Summary("Get a book"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[Books] secured=true deprecated=false
```

<a id="Deprecated"></a>

#### func Deprecated

```go
func Deprecated() RouteOption
```

Deprecated marks the route deprecated in the OpenAPI document.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Deprecated())
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[] secured=true deprecated=true
```

<a id="Description"></a>

#### func Description

```go
func Description(markdown string) RouteOption
```

Description sets the OpenAPI description, in Markdown.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Description("Returns one of **your** books."))
}})
if err != nil {
	panic(err)
}
fmt.Println(api.OpenAPI().Paths["/v1/books/{id}"].Get.Description)
```

Output:

```text
Returns one of **your** books.
```

<a id="Errors"></a>

#### func Errors

```go
func Errors(statuses ...int) RouteOption
```

Errors documents error statuses the route returns, in addition to those its guards document.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Errors(http.StatusNotFound))
}})
if err != nil {
	panic(err)
}
responses := api.OpenAPI().Paths["/v1/books/{id}"].Get.Responses
fmt.Println(responses["401"] != nil, responses["404"] != nil)
```

Output:

```text
true true
```

<a id="OperationID"></a>

#### func OperationID

```go
func OperationID(id string) RouteOption
```

OperationID sets the operation ID. Without it, the ID is the module's name followed by the one Huma generates from the method and path, such as "books-post-v1-books". Operation IDs are public API: client generators name their functions after them.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.OperationID("books-get"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get summary="Get v1 books by ID" tags=[] secured=true deprecated=false
```

<a id="Status"></a>

#### func Status

```go
func Status(code int) RouteOption
```

Status sets the success status, such as http.StatusCreated.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
mux, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
	gorbital.Delete(r, "/v1/books/{id}", removeBook, gorbital.Status(http.StatusNoContent))
}})
if err != nil {
	panic(err)
}
fmt.Println(call(mux, http.MethodDelete, "/v1/books/bok_1", true))
```

Output:

```text
204
```

<a id="Summary"></a>

#### func Summary

```go
func Summary(s string) RouteOption
```

Summary sets the OpenAPI summary. Without it, Huma generates one from the method and path.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Summary("Get a book"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[] secured=true deprecated=false
```

<a id="Tags"></a>

#### func Tags

```go
func Tags(tags ...string) RouteOption
```

Tags sets the OpenAPI tags. Without it, a route has none.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
routesExample(func(r *gorbital.Router) {
	gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Tags("Books", "Library"))
}, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[Books Library] secured=true deprecated=false
```

<a id="Use"></a>

#### func Use

```go
func Use(middlewares ...func(http.Handler) http.Handler) RouteOption
```

Use adds middleware to a route, or to every route of a group. Middleware runs in the order given, after the module's and the group's middleware and before the route's guards and input parsing (ADR-0082). Any standard middleware works:

```go
books := r.Group("/v1/books", gorbital.Use(requireClientVersion("2.4.0")))
```

A route can't remove middleware its group added: put routes that need different middleware in their own group.

Route middleware needs an API built on Huma's humago adapter, as openapi.New builds it.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// requireClientVersion refuses mobile apps older than min.
requireClientVersion := func(min string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-App-Version") < min {
				httpx.WriteProblem(w, r, httpx.NewProblem(http.StatusUpgradeRequired, "app_outdated", "update the app to continue"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

mux, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		books := r.Group("/v1/books", gorbital.Use(requireClientVersion("2.4.0")))
		gorbital.Get(books, "/{id}", findBook)
	},
})
if err != nil {
	panic(err)
}
for _, version := range []string{"2.3.9", "2.4.0"} {
	req := httptest.NewRequest(http.MethodGet, "/v1/books/bok_1", nil)
	req.Header.Set("X-App-Version", version)
	req = req.WithContext(actor.With(req.Context(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	fmt.Println(version, rec.Code)
}
```

Output:

```text
2.3.9 426
2.4.0 200
```

<a id="Router"></a>

### type Router

```go
type Router struct {
	// contains filtered or unexported fields
}
```

A Router registers a module's routes under a path prefix with shared options. [Mount](#Mount) passes each module's Routes a Router for that module.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/books/{id}", findBook)
	},
})
if err != nil {
	panic(err)
}
describe(api, http.MethodGet, "/v1/books/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[] secured=true deprecated=false
```

<a id="Router.Group"></a>

#### func (*Router) Group

```go
func (r *Router) Group(prefix string, opts ...RouteOption) *Router
```

Group returns a Router for the routes under prefix, which is empty or starts with a slash, with opts added to the options of r.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
_, api, mapper := newAPI()
err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
	Name: "books",
	Routes: func(r *gorbital.Router, d gorbital.Deps) {
		v1 := r.Group("/v1", gorbital.Tags("Books"))
		books := v1.Group("/books")
		gorbital.Get(books, "/{id}", findBook)
		gorbital.Get(v1.Group("/catalog", guard.Public()), "/{id}", findBook)
	},
})
if err != nil {
	panic(err)
}
describe(api, http.MethodGet, "/v1/books/{id}")
describe(api, http.MethodGet, "/v1/catalog/{id}")
```

Output:

```text
GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[Books] secured=true deprecated=false
GET /v1/catalog/{id} id=books-get-v1-catalog-by-id summary="Get v1 catalog by ID" tags=[Books] secured=false deprecated=false
```
