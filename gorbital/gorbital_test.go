package gorbital_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/settings"
)

type bookInput struct {
	ID string `path:"id" maxLength:"64"`
}

type bookOutput struct {
	Body struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
}

var errBookNotFound = errors.New("books: not found")

func getBook(_ context.Context, in *bookInput) (*bookOutput, error) {
	if in.ID == "missing" {
		return nil, errBookNotFound
	}
	out := &bookOutput{}
	out.Body.ID, out.Body.Title = in.ID, "Dune"
	return out, nil
}

type createInput struct {
	Body struct {
		Title string `json:"title" minLength:"1"`
	}
}

func createBook(_ context.Context, in *createInput) (*bookOutput, error) {
	out := &bookOutput{}
	out.Body.ID, out.Body.Title = "bok_1", in.Body.Title
	return out, nil
}

// testAPI is an API as the golden apps build it: humago on a ServeMux, the
// bearer scheme, and problem+json errors from the mapper.
type testAPI struct {
	mux    *http.ServeMux
	api    huma.API
	mapper *httpx.Mapper
}

func newTestAPI(t testing.TB, opts ...openapi.Option) testAPI {
	t.Helper()
	mux := http.NewServeMux()
	api := openapi.New(mux, "test", "1.0.0", opts...)
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	openapi.InstallErrors(mapper)
	return testAPI{mux: mux, api: api, mapper: mapper}
}

func withBearer() openapi.Option { return openapi.WithBearerAuth("session token") }

// signedIn sets a user actor, as the auth middleware does.
func signedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := actor.With(r.Context(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func booksModule() gorbital.Module {
	return gorbital.Module{
		Name: "books",
		Errors: []httpx.Mapping{
			{Err: errBookNotFound, Status: http.StatusNotFound, Code: "book_not_found", Detail: "no book has this ID"},
		},
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			books := r.Group("/v1/books", gorbital.Tags("Books"))
			gorbital.Get(books, "/{id}", getBook, gorbital.Errors(http.StatusNotFound))
			gorbital.Post(books, "", createBook, gorbital.Status(http.StatusCreated))
			catalog := r.Group("/v1/catalog", guard.Public())
			gorbital.Get(catalog, "/{id}", getBook)
		},
	}
}

type problem struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	RequestID string `json:"request_id"`
}

func do(t *testing.T, h http.Handler, method, target, body string) (*httptest.ResponseRecorder, problem) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var p problem
	if strings.HasPrefix(rec.Header().Get("Content-Type"), httpx.ProblemContentType) {
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatalf("decode problem: %v; body %s", err, rec.Body)
		}
	}
	return rec, p
}

func TestMountDeniesByDefault(t *testing.T) {
	a := newTestAPI(t, withBearer())
	if err := gorbital.Mount(a.api, a.mapper, gorbital.Deps{}, booksModule()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		handler  http.Handler
		method   string
		target   string
		body     string
		wantCode int
		wantProb string
	}{
		{"anonymous on a protected route", a.mux, http.MethodGet, "/v1/books/bok_1", "", http.StatusUnauthorized, "unauthenticated"},
		{"signed in on a protected route", signedIn(a.mux), http.MethodGet, "/v1/books/bok_1", "", http.StatusOK, ""},
		{"anonymous on a public group", a.mux, http.MethodGet, "/v1/catalog/bok_1", "", http.StatusOK, ""},
		{"anonymous is refused before the body is validated", a.mux, http.MethodPost, "/v1/books", `{"title":""}`, http.StatusUnauthorized, "unauthenticated"},
		{"signed in reaches validation", signedIn(a.mux), http.MethodPost, "/v1/books", `{"title":""}`, http.StatusUnprocessableEntity, "validation_failed"},
		{"route status option", signedIn(a.mux), http.MethodPost, "/v1/books", `{"title":"Dune"}`, http.StatusCreated, ""},
		{"module error mapping", signedIn(a.mux), http.MethodGet, "/v1/books/missing", "", http.StatusNotFound, "book_not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, p := do(t, tt.handler, tt.method, tt.target, tt.body)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.wantCode, rec.Body)
			}
			if p.Code != tt.wantProb {
				t.Errorf("problem code = %q, want %q", p.Code, tt.wantProb)
			}
		})
	}
}

func TestMountAnonymousActorIsRefused(t *testing.T) {
	a := newTestAPI(t, withBearer())
	if err := gorbital.Mount(a.api, a.mapper, gorbital.Deps{}, booksModule()); err != nil {
		t.Fatal(err)
	}
	anonymous := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mux.ServeHTTP(w, r.WithContext(actor.With(r.Context(), actor.Anonymous)))
	})
	if rec, _ := do(t, anonymous, http.MethodGet, "/v1/books/bok_1", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// operation returns the OpenAPI operation for method and path.
func operation(t *testing.T, api huma.API, method, path string) *huma.Operation {
	t.Helper()
	item := api.OpenAPI().Paths[path]
	if item == nil {
		t.Fatalf("no path %s in the document", path)
	}
	var op *huma.Operation
	switch method {
	case http.MethodGet:
		op = item.Get
	case http.MethodPost:
		op = item.Post
	}
	if op == nil {
		t.Fatalf("no %s %s in the document", method, path)
	}
	return op
}

func TestMountDocumentsSecurity(t *testing.T) {
	a := newTestAPI(t, withBearer())
	if err := gorbital.Mount(a.api, a.mapper, gorbital.Deps{}, booksModule()); err != nil {
		t.Fatal(err)
	}

	protected := operation(t, a.api, http.MethodGet, "/v1/books/{id}")
	if len(protected.Security) != 1 || protected.Security[0][openapi.BearerScheme] == nil {
		t.Errorf("protected route security = %v, want bearer", protected.Security)
	}
	for _, status := range []string{"401", "404"} {
		if protected.Responses[status] == nil {
			t.Errorf("protected route documents no %s response", status)
		}
	}
	if protected.OperationID != "books-get-v1-books-by-id" {
		t.Errorf("operation ID = %q", protected.OperationID)
	}
	if !slices.Equal(protected.Tags, []string{"Books"}) {
		t.Errorf("tags = %v, want the group's", protected.Tags)
	}

	public := operation(t, a.api, http.MethodGet, "/v1/catalog/{id}")
	if len(public.Security) != 0 {
		t.Errorf("public route security = %v, want none", public.Security)
	}
	if public.Responses["401"] != nil {
		t.Error("public route documents a 401 response")
	}
}

// TestMountMatchesHandWrittenOperation pins the contract: a route registered
// through gorbital documents exactly what the equivalent hand-written Huma
// registration with the v0.1 signedIn wrapper documents, plus the list of
// guards the route runs.
func TestMountMatchesHandWrittenOperation(t *testing.T) {
	viaGorbital := newTestAPI(t, withBearer())
	err := gorbital.Mount(viaGorbital.api, viaGorbital.mapper, gorbital.Deps{}, gorbital.Module{
		Name: "books",
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			gorbital.Post(r, "/v1/books", createBook,
				gorbital.OperationID("books-create"), gorbital.Summary("Create a book"),
				gorbital.Tags("Books"), gorbital.Status(http.StatusCreated), gorbital.Errors(http.StatusConflict))
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	byHand := newTestAPI(t, withBearer())
	huma.Register(byHand.api, huma.Operation{
		OperationID: "books-create", Method: http.MethodPost, Path: "/v1/books",
		Summary: "Create a book", Tags: []string{"Books"}, DefaultStatus: http.StatusCreated,
		Security: openapi.Bearer, Errors: []int{http.StatusUnauthorized, http.StatusConflict},
		Extensions: map[string]any{"x-gorbital-guards": []string{"authenticated"}},
	}, createBook)

	got, err := json.Marshal(operation(t, viaGorbital.api, http.MethodPost, "/v1/books"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(operation(t, byHand.api, http.MethodPost, "/v1/books"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("operation differs from the hand-written one\n got: %s\nwant: %s", got, want)
	}
}

func route(name string, register func(r *gorbital.Router)) gorbital.Module {
	return gorbital.Module{Name: name, Routes: func(r *gorbital.Router, _ gorbital.Deps) { register(r) }}
}

func TestMountErrors(t *testing.T) {
	var nilHandler func(context.Context, *bookInput) (*bookOutput, error)
	tests := []struct {
		name    string
		bearer  bool
		mapper  bool
		modules []gorbital.Module
		want    string
	}{
		{"unnamed module", true, true, []gorbital.Module{{}}, "module 0 has no name"},
		{"invalid name", true, true, []gorbital.Module{{Name: "Books"}}, `module name "Books" must be lowercase snake_case`},
		{"duplicate name", true, true, []gorbital.Module{{Name: "books"}, {Name: "books"}}, `two modules are named "books"`},
		{
			"same route in two modules, parameters named differently", true, true,
			[]gorbital.Module{
				route("books", func(r *gorbital.Router) { gorbital.Get(r, "/v1/books/{id}", getBook) }),
				route("shelves", func(r *gorbital.Router) {
					gorbital.Get(r, "/v1/books/{bookID}", getBook, gorbital.OperationID("other"))
				}),
			},
			`GET /v1/books/{bookID} is registered by modules "books" and "shelves"`,
		},
		{
			"same operation ID", true, true,
			[]gorbital.Module{
				route("books", func(r *gorbital.Router) { gorbital.Get(r, "/v1/books/{id}", getBook, gorbital.OperationID("get")) }),
				route("shelves", func(r *gorbital.Router) { gorbital.Get(r, "/v1/shelves/{id}", getBook, gorbital.OperationID("get")) }),
			},
			`operation ID "get" is used by modules "books" and "shelves"`,
		},
		{"path without a slash", true, true, []gorbital.Module{route("books", func(r *gorbital.Router) { gorbital.Get(r, "v1/books/{id}", getBook) })}, `"v1/books/{id}" must start with a slash`},
		{"group with a trailing slash", true, true, []gorbital.Module{route("books", func(r *gorbital.Router) { gorbital.Get(r.Group("/v1/"), "/books/{id}", getBook) })}, `"/v1/" must not end with a slash`},
		{"empty path", true, true, []gorbital.Module{route("books", func(r *gorbital.Router) { gorbital.Get(r, "", getBook) })}, "path is empty"},
		{"nil handler", true, true, []gorbital.Module{route("books", func(r *gorbital.Router) { gorbital.Get(r, "/v1/books/{id}", nilHandler) })}, "handler is nil"},
		{"protected route without the bearer scheme", false, true, []gorbital.Module{route("books", func(r *gorbital.Router) { gorbital.Get(r, "/v1/books/{id}", getBook) })}, "declares no bearer security scheme"},
		{"errors without a mapper", true, false, []gorbital.Module{{Name: "books", Errors: booksModule().Errors}}, `module "books" maps errors, but the mapper is nil`},
		{
			"mapping the mapper refuses", true, true,
			[]gorbital.Module{{Name: "books", Errors: []httpx.Mapping{{Err: errBookNotFound, Status: 200, Code: "book_not_found"}}}},
			`module "books": httpx: mapping "book_not_found": status 200 is not an error status`,
		},
		{
			"input Huma can't register", true, true,
			[]gorbital.Module{route("books", func(r *gorbital.Router) {
				gorbital.Post(r, "/v1/books", func(context.Context, *string) (*bookOutput, error) { return nil, nil })
			})},
			`module "books": POST /v1/books:`,
		},
		{"panic in Routes", true, true, []gorbital.Module{route("books", func(*gorbital.Router) { panic("boom") })}, `module "books": routes: boom`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var opts []openapi.Option
			if tt.bearer {
				opts = append(opts, withBearer())
			}
			a := newTestAPI(t, opts...)
			mapper := a.mapper
			if !tt.mapper {
				mapper = nil
			}
			err := gorbital.Mount(a.api, mapper, gorbital.Deps{}, tt.modules...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Mount() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestMountPublicRouteNeedsNoBearerScheme(t *testing.T) {
	a := newTestAPI(t)
	m := route("health", func(r *gorbital.Router) { gorbital.Get(r, "/v1/ping/{id}", getBook, guard.Public()) })
	if err := gorbital.Mount(a.api, a.mapper, gorbital.Deps{}, m); err != nil {
		t.Fatal(err)
	}
}

func TestMountTagsLoggerWithModule(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	a := newTestAPI(t, withBearer())
	m := gorbital.Module{Name: "books", Routes: func(_ *gorbital.Router, d gorbital.Deps) { d.Logger.Info("routes") }}
	if err := gorbital.Mount(a.api, a.mapper, gorbital.Deps{Logger: logger}, m); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"module":"books"`) {
		t.Errorf("log = %s, want module=books", buf.String())
	}

	m.Routes = func(_ *gorbital.Router, d gorbital.Deps) { d.Logger.Info("no logger configured") }
	if err := gorbital.Mount(newTestAPI(t).api, a.mapper, gorbital.Deps{}, m); err != nil {
		t.Fatal(err)
	}
}

// recordingCatalog records declared permissions and panics on duplicates, as
// *auth.Catalog does.
type recordingCatalog struct{ names []string }

func (c *recordingCatalog) Permission(name, _ string) {
	if slices.Contains(c.names, name) {
		panic("permission " + name + " declared twice")
	}
	c.names = append(c.names, name)
}

func TestDeclare(t *testing.T) {
	var pageSize *settings.Setting[int]
	var covers *flags.Flag
	books := gorbital.Module{
		Name: "books",
		Permissions: []gorbital.Permission{
			{Name: "books.book.read", Description: "Read books", Roles: []string{"user", "viewer"}},
			{Name: "books.book.write", Description: "Change books", Roles: []string{"user"}},
		},
		Settings: func(r *settings.Registry) { pageSize = settings.Int(r, "books.page_size", 20) },
		Flags:    func(r *flags.Registry) { covers = flags.Bool(r, "books.covers") },
	}
	shelves := gorbital.Module{
		Name:        "shelves",
		Permissions: []gorbital.Permission{{Name: "shelves.shelf.read", Roles: []string{"user"}}},
	}

	catalog := &recordingCatalog{}
	reg, freg := settings.NewRegistry(), flags.NewRegistry()
	if err := gorbital.Declare(gorbital.Declarations{Permissions: catalog, Settings: reg, Flags: freg}, books, shelves); err != nil {
		t.Fatal(err)
	}
	if want := []string{"books.book.read", "books.book.write", "shelves.shelf.read"}; !slices.Equal(catalog.names, want) {
		t.Errorf("declared permissions = %v, want %v", catalog.names, want)
	}
	if pageSize == nil || !slices.Contains(reg.Keys(), "books.page_size") {
		t.Error("the module's setting was not declared")
	}
	if covers == nil {
		t.Error("the module's flag was not declared")
	}
	if got, want := gorbital.Grants("user", books, shelves), []string{"books.book.read", "books.book.write", "shelves.shelf.read"}; !slices.Equal(got, want) {
		t.Errorf("Grants(user) = %v, want %v", got, want)
	}
	if got, want := gorbital.Grants("viewer", books, shelves), []string{"books.book.read"}; !slices.Equal(got, want) {
		t.Errorf("Grants(viewer) = %v, want %v", got, want)
	}
	if got := gorbital.Grants("admin", books); got != nil {
		t.Errorf("Grants(admin) = %v, want none", got)
	}
}

func TestDeclareErrors(t *testing.T) {
	declaresSetting := func(name, key string) gorbital.Module {
		return gorbital.Module{Name: name, Settings: func(r *settings.Registry) { settings.Int(r, key, 1) }}
	}
	tests := []struct {
		name    string
		d       gorbital.Declarations
		modules []gorbital.Module
		want    string
	}{
		{
			"permission in two modules", gorbital.Declarations{},
			[]gorbital.Module{
				{Name: "books", Permissions: []gorbital.Permission{{Name: "books.book.read"}}},
				{Name: "shelves", Permissions: []gorbital.Permission{{Name: "books.book.read"}}},
			},
			`permission "books.book.read" is declared by modules "books" and "shelves"`,
		},
		{"settings without a registry", gorbital.Declarations{}, []gorbital.Module{declaresSetting("books", "books.page_size")}, `module "books" declares settings, but Declarations.Settings is nil`},
		{"flags without a registry", gorbital.Declarations{}, []gorbital.Module{{Name: "books", Flags: func(r *flags.Registry) { flags.Bool(r, "books.covers") }}}, `module "books" declares flags, but Declarations.Flags is nil`},
		{
			"invalid setting reported with its module", gorbital.Declarations{Settings: settings.NewRegistry()},
			[]gorbital.Module{declaresSetting("books", "Page Size")},
			`module "books": settings: settings: key "Page Size" must be dotted lowercase`,
		},
		{
			"setting declared by two modules", gorbital.Declarations{Settings: settings.NewRegistry()},
			[]gorbital.Module{declaresSetting("books", "shared.limit"), declaresSetting("shelves", "shared.limit")},
			`module "shelves": settings: settings: shared.limit: declared twice`,
		},
		{"duplicate module", gorbital.Declarations{}, []gorbital.Module{{Name: "books"}, {Name: "books"}}, `two modules are named "books"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gorbital.Declare(tt.d, tt.modules...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Declare() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
