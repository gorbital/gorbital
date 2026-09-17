package gorbital_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/settings"
)

// The examples build an API as a v0.1 app's routes.go does: Huma on a
// ServeMux with the bearer scheme, and problem+json errors from a mapper.
func newAPI() (*http.ServeMux, huma.API, *httpx.Mapper) {
	mux := http.NewServeMux()
	api := openapi.New(mux, "Shelfie", "1.0.0", openapi.WithBearerAuth("Session token"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		panic(err)
	}
	openapi.InstallErrors(mapper)
	return mux, api, mapper
}

// call sends a request, as the signed-in user usr_1 when signedIn, and returns
// the status and the problem code, if any.
func call(h http.Handler, method, target string, signedIn bool) string {
	req := httptest.NewRequest(method, target, nil)
	if signedIn {
		req = req.WithContext(actor.With(req.Context(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"}))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var p struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &p) == nil && p.Code != "" {
		return fmt.Sprintf("%d %s", rec.Code, p.Code)
	}
	return fmt.Sprint(rec.Code)
}

// describe prints what the OpenAPI document says about an operation.
func describe(api huma.API, method, path string) {
	item := api.OpenAPI().Paths[path]
	op := map[string]*huma.Operation{
		http.MethodGet: item.Get, http.MethodPost: item.Post, http.MethodPut: item.Put,
		http.MethodPatch: item.Patch, http.MethodDelete: item.Delete,
	}[method]
	fmt.Printf("%s %s id=%s summary=%q tags=%v secured=%t deprecated=%t\n",
		method, path, op.OperationID, op.Summary, op.Tags, len(op.Security) > 0, op.Deprecated)
}

type Book struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type bookIDInput struct {
	ID string `path:"id"`
}

type bookBody struct{ Body Book }

type newBookInput struct {
	Body struct {
		Title string `json:"title" minLength:"1"`
	}
}

var ErrBookNotFound = errors.New("books: book not found")

func findBook(_ context.Context, in *bookIDInput) (*bookBody, error) {
	if in.ID != "bok_1" {
		return nil, ErrBookNotFound
	}
	return &bookBody{Body: Book{ID: in.ID, Title: "Dune"}}, nil
}

func addBook(_ context.Context, in *newBookInput) (*bookBody, error) {
	return &bookBody{Body: Book{ID: "bok_2", Title: in.Body.Title}}, nil
}

func removeBook(context.Context, *bookIDInput) (*struct{}, error) { return nil, nil }

func ExampleModule() {
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
	// Output:
	// 200
	// 404 book_not_found
}

func ExampleMount() {
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
	// Output:
	// 401 unauthenticated
	// 200
	// 200
}

func ExampleMount_duplicateRoute() {
	_, api, mapper := newAPI()
	route := func(name string) gorbital.Module {
		return gorbital.Module{Name: name, Routes: func(r *gorbital.Router, d gorbital.Deps) {
			gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.OperationID(name+"-get-book"))
		}}
	}
	err := gorbital.Mount(api, mapper, gorbital.Deps{}, route("books"), route("library"))
	fmt.Println(err)
	// Output:
	// gorbital: GET /v1/books/{id} is registered by modules "books" and "library"
}

func ExampleDeclare() {
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
	// Output:
	// [books.page_size] true
	// [books.book.read]
}

func ExampleDeclarations() {
	books := gorbital.Module{
		Name:  "books",
		Flags: func(r *flags.Registry) { flags.Bool(r, "books.covers", flags.Describe("Show cover images")) },
	}
	err := gorbital.Declare(gorbital.Declarations{}, books)
	fmt.Println(err)
	// Output:
	// gorbital: module "books" declares flags, but Declarations.Flags is nil
}

func ExampleGrants() {
	books := gorbital.Module{Name: "books", Permissions: []gorbital.Permission{
		{Name: "books.book.read", Roles: []string{"user", "viewer"}},
		{Name: "books.book.write", Roles: []string{"user"}},
	}}
	shelves := gorbital.Module{Name: "shelves", Permissions: []gorbital.Permission{
		{Name: "shelves.shelf.read", Roles: []string{"viewer"}},
	}}
	fmt.Println(gorbital.Grants("user", books, shelves))
	fmt.Println(gorbital.Grants("viewer", books, shelves))
	// Output:
	// [books.book.read books.book.write]
	// [books.book.read shelves.shelf.read]
}

func ExamplePermission() {
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
	// Output:
	// 1 books.book.write
}

// allowList is a PermissionDeclarer that only records names, for an app
// that checks permissions without *auth.Catalog.
type allowList []string

func (l *allowList) Permission(name, _ string) { *l = append(*l, name) }

func ExamplePermissionDeclarer() {
	var declared allowList
	books := gorbital.Module{Name: "books", Permissions: []gorbital.Permission{{Name: "books.book.read"}, {Name: "books.book.write"}}}
	if err := gorbital.Declare(gorbital.Declarations{Permissions: &declared}, books); err != nil {
		panic(err)
	}
	fmt.Println(declared)
	// Output:
	// [books.book.read books.book.write]
}

func ExampleDeps() {
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
	// Output:
	// <nil>
}

func ExampleRouter() {
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
	// Output:
	// GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[] secured=true deprecated=false
}

func ExampleRouter_Group() {
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
	// Output:
	// GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[Books] secured=true deprecated=false
	// GET /v1/catalog/{id} id=books-get-v1-catalog-by-id summary="Get v1 catalog by ID" tags=[Books] secured=false deprecated=false
}

// routesExample mounts one module with routes and describes an operation.
func routesExample(routes func(r *gorbital.Router), method, path string) {
	_, api, mapper := newAPI()
	err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
		Name:   "books",
		Routes: func(r *gorbital.Router, d gorbital.Deps) { routes(r) },
	})
	if err != nil {
		panic(err)
	}
	describe(api, method, path)
}

func ExampleGet() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Summary("Get a book"))
	}, http.MethodGet, "/v1/books/{id}")
	// Output:
	// GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[] secured=true deprecated=false
}

func ExamplePost() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Post(r, "/v1/books", addBook, gorbital.Status(http.StatusCreated))
	}, http.MethodPost, "/v1/books")
	// Output:
	// POST /v1/books id=books-post-v1-books summary="Post v1 books" tags=[] secured=true deprecated=false
}

func ExamplePut() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Put(r, "/v1/books/{id}/title", findBook, gorbital.Summary("Replace a book's title"))
	}, http.MethodPut, "/v1/books/{id}/title")
	// Output:
	// PUT /v1/books/{id}/title id=books-put-v1-books-by-id-title summary="Replace a book's title" tags=[] secured=true deprecated=false
}

func ExamplePatch() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Patch(r, "/v1/books/{id}", findBook, gorbital.Summary("Update a book"))
	}, http.MethodPatch, "/v1/books/{id}")
	// Output:
	// PATCH /v1/books/{id} id=books-patch-v1-books-by-id summary="Update a book" tags=[] secured=true deprecated=false
}

func ExampleDelete() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Delete(r, "/v1/books/{id}", removeBook, gorbital.Status(http.StatusNoContent))
	}, http.MethodDelete, "/v1/books/{id}")
	// Output:
	// DELETE /v1/books/{id} id=books-delete-v1-books-by-id summary="Delete v1 books by ID" tags=[] secured=true deprecated=false
}

func ExampleRouteOption() {
	// Options on a group apply to its routes first; a route's own options
	// come after and win.
	routesExample(func(r *gorbital.Router) {
		books := r.Group("/v1/books", gorbital.Tags("Books"), gorbital.Summary("A books operation"))
		gorbital.Get(books, "/{id}", findBook, gorbital.Summary("Get a book"))
	}, http.MethodGet, "/v1/books/{id}")
	// Output:
	// GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[Books] secured=true deprecated=false
}

func ExampleTags() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Tags("Books", "Library"))
	}, http.MethodGet, "/v1/books/{id}")
	// Output:
	// GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[Books Library] secured=true deprecated=false
}

func ExampleSummary() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Summary("Get a book"))
	}, http.MethodGet, "/v1/books/{id}")
	// Output:
	// GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get a book" tags=[] secured=true deprecated=false
}

func ExampleDescription() {
	_, api, mapper := newAPI()
	err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Description("Returns one of **your** books."))
	}})
	if err != nil {
		panic(err)
	}
	fmt.Println(api.OpenAPI().Paths["/v1/books/{id}"].Get.Description)
	// Output:
	// Returns one of **your** books.
}

func ExampleOperationID() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.OperationID("books-get"))
	}, http.MethodGet, "/v1/books/{id}")
	// Output:
	// GET /v1/books/{id} id=books-get summary="Get v1 books by ID" tags=[] secured=true deprecated=false
}

func ExampleStatus() {
	mux, api, mapper := newAPI()
	err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Delete(r, "/v1/books/{id}", removeBook, gorbital.Status(http.StatusNoContent))
	}})
	if err != nil {
		panic(err)
	}
	fmt.Println(call(mux, http.MethodDelete, "/v1/books/bok_1", true))
	// Output:
	// 204
}

func ExampleErrors() {
	_, api, mapper := newAPI()
	err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Errors(http.StatusNotFound))
	}})
	if err != nil {
		panic(err)
	}
	responses := api.OpenAPI().Paths["/v1/books/{id}"].Get.Responses
	fmt.Println(responses["401"] != nil, responses["404"] != nil)
	// Output:
	// true true
}

func ExampleAuthenticateAfterInput() {
	mux, api, mapper := newAPI()
	err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{Name: "books", Routes: func(r *gorbital.Router, d gorbital.Deps) {
		gorbital.Post(r, "/v1/books", addBook, gorbital.AuthenticateAfterInput())
	}})
	if err != nil {
		panic(err)
	}
	post := func(body string) string {
		req := httptest.NewRequest(http.MethodPost, "/v1/books", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var p struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		return fmt.Sprintf("%d %s", rec.Code, p.Code)
	}
	// Without credentials: invalid input is answered first, valid input is
	// refused.
	fmt.Println(post(`{"title":""}`))
	fmt.Println(post(`{"title":"Dune"}`))
	// Output:
	// 422 validation_failed
	// 401 unauthenticated
}

func ExampleDeprecated() {
	routesExample(func(r *gorbital.Router) {
		gorbital.Get(r, "/v1/books/{id}", findBook, gorbital.Deprecated())
	}, http.MethodGet, "/v1/books/{id}")
	// Output:
	// GET /v1/books/{id} id=books-get-v1-books-by-id summary="Get v1 books by ID" tags=[] secured=true deprecated=true
}

func ExampleUse() {
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
	// Output:
	// 2.3.9 426
	// 2.4.0 200
}

func ExampleTimeout() {
	mux, api, mapper := newAPI()
	err := gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
		Name: "reports",
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			slow := func(ctx context.Context, _ *struct{}) (*struct{}, error) {
				<-ctx.Done() // a query that respects its context stops here
				return nil, ctx.Err()
			}
			gorbital.Get(r, "/v1/reports/yearly", slow, guard.Public(), gorbital.Timeout(20*time.Millisecond))
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(call(mux, http.MethodGet, "/v1/reports/yearly", false))
	// Output:
	// 503 request_timeout
}
