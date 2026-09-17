package routes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func build(t *testing.T, app string) List {
	t.Helper()
	dir := filepath.Join("testdata", app)
	doc, err := os.ReadFile(filepath.Join(dir, "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := Build(app, dir, doc, SourceFile)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func find(t *testing.T, l List, method, path string) Route {
	t.Helper()
	for _, r := range l.Routes {
		if r.Method == method && r.Path == path {
			return r
		}
	}
	t.Fatalf("no route %s %s in %+v", method, path, l.Routes)
	return Route{}
}

func TestBuildMainApp(t *testing.T) {
	l := build(t, "mainapp")
	if !l.GuardsKnown || l.Total != 6 || l.Public != 2 || l.Source != SourceFile {
		t.Errorf("list = %+v", l)
	}
	var order []string
	for _, r := range l.Routes {
		order = append(order, r.Method+" "+r.Path)
	}
	if want := []string{"POST /v1/books", "GET /v1/books/{id}", "DELETE /v1/books/{id}", "GET /v1/ping", "GET /v1/public/{id}", "GET /version"}; !slices.Equal(order, want) {
		t.Errorf("order = %v", order)
	}

	get := find(t, l, "GET", "/v1/books/{id}")
	routesGo := "internal/modules/catalog/delivery/routes.go"
	if get.Module != "books_catalog" || get.Handler != "h.get" || get.Source.String() != routesGo+":20" || get.HandlerSource.String() != routesGo+":28" ||
		!slices.Equal(get.Guards, []string{"authenticated", "permission:catalog.book.read"}) || get.Public ||
		!slices.Equal(get.Middleware, []string{"delivery.Timing", `requireVersion("2.4.0")`, "Timing", "cacheFor(60)"}) || !slices.Equal(get.Tags, []string{"Books"}) {
		t.Errorf("GET /v1/books/{id} = %+v", get)
	}
	// Matched by method and path, and by the literal operation ID.
	if post := find(t, l, "POST", "/v1/books"); post.Handler != "h.create" || post.Source.String() != routesGo+":21" {
		t.Errorf("POST /v1/books = %+v", post)
	}
	if public := find(t, l, "GET", "/v1/public/{id}"); !public.Public || public.Source.String() != routesGo+":22" || !slices.Equal(public.Middleware, []string{"delivery.Timing"}) {
		t.Errorf("GET /v1/public/{id} = %+v", public)
	}
	if del := find(t, l, "DELETE", "/v1/books/{id}"); !del.Deprecated || del.HandlerSource.String() != routesGo+":32" {
		t.Errorf("DELETE /v1/books/{id} = %+v", del)
	}
	if ping := find(t, l, "GET", "/v1/ping"); ping.Handler != "func(…)" || ping.HandlerSource.String() != routesGo+":24" {
		t.Errorf("GET /v1/ping = %+v", ping)
	}
	// A library route: no source, and a warning says so.
	if version := find(t, l, "GET", "/version"); version.Source != nil || version.Module != "" || !version.Public || len(version.Guards) != 0 {
		t.Errorf("GET /version = %+v", version)
	}
	if len(l.Warnings) != 1 || !strings.Contains(l.Warnings[0], "1 of 6 routes have no source position") {
		t.Errorf("warnings = %q", l.Warnings)
	}

	public := l.Filter("", true, false)
	if public.Total != 2 || public.Public != 2 {
		t.Errorf("Filter(public) = %+v", public)
	}
	if catalog := l.Filter("books_catalog", false, false); catalog.Total != 5 {
		t.Errorf("Filter(books_catalog) = %d routes", catalog.Total)
	}
	// Only the app's routes: the library's /version and its warning go.
	if app := l.Filter("", false, true); app.Total != 5 || app.Public != 1 || len(app.Warnings) != 0 || len(l.Warnings) != 1 {
		t.Errorf("Filter(app) = %+v, warnings before %q", app, l.Warnings)
	}

	// The JSON form: every array present, missing positions null.
	data, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"guards_known":true`, `"source":null`, `"middleware":[]`, `"handler_source":{"file":"internal/modules/catalog/delivery/routes.go","line":28}`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("JSON lacks %s:\n%s", want, data)
		}
	}
}

func TestBuildV01App(t *testing.T) {
	l := build(t, "v01app")
	if l.GuardsKnown || l.Total != 3 || l.Public != 1 {
		t.Errorf("list = %+v", l)
	}
	create := find(t, l, "POST", "/v1/projects")
	projectsGo := "internal/modules/projects/delivery/projects.go"
	if create.Module != "projects" || create.Handler != "h.create" || create.Source.String() != projectsGo+":15" || create.HandlerSource.String() != projectsGo+":21" || create.Public {
		t.Errorf("POST /v1/projects = %+v", create)
	}
	if list := find(t, l, "GET", "/v1/projects"); list.Source.String() != projectsGo+":18" {
		t.Errorf("GET /v1/projects = %+v", list)
	}
	if !slices.ContainsFunc(l.Warnings, func(w string) bool { return strings.Contains(w, "no x-gorbital-guards") }) {
		t.Errorf("warnings = %q", l.Warnings)
	}
}

func TestFromOpenAPIErrors(t *testing.T) {
	for doc, want := range map[string]string{
		`not json`:          "isn't valid JSON",
		`{"swagger":"2.0"}`: "not an OpenAPI 3 document",
		`{"openapi":"3.1.0","paths":{"/x":{"get":[]}}}`: "GET /x",
	} {
		if _, _, err := FromOpenAPI([]byte(doc)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("FromOpenAPI(%s) error = %v, want %q", doc, err, want)
		}
	}
}

// FuzzFromOpenAPI: any input is refused or listed without panicking, and
// every listed route has a known method.
func FuzzFromOpenAPI(f *testing.F) {
	for _, seed := range []string{`{"openapi":"3.1.0","paths":{"/x":{"get":{"x-gorbital-guards":["public"]}}}}`, `{}`, `{"openapi":"3.0.0","paths":{"/":{"parameters":[]}}}`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, doc []byte) {
		routes, _, err := FromOpenAPI(doc)
		if err != nil {
			return
		}
		for _, r := range routes {
			if !slices.Contains(methodOrder, r.Method) || r.Guards == nil || r.Tags == nil {
				t.Fatalf("route %+v", r)
			}
		}
	})
}
