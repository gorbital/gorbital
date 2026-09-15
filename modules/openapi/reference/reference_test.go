package reference_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"apistock.dev/modules/openapi/reference"
)

const shopSpec = `{
  "openapi": "3.1.0",
  "info": {"title": "Shop API", "version": "1.2.0", "description": "Sells **things**. <script>alert(1)</script> See [the guide](https://example.com/guide)."},
  "components": {
    "securitySchemes": {"bearer": {"type": "http", "scheme": "bearer", "description": "Token from ` + "`POST /login`" + `."}},
    "schemas": {
      "Problem": {"type": "object", "required": ["code"], "properties": {"status": {"type": "integer"}, "code": {"type": "string", "examples": ["order_taken"]}, "detail": {"type": "string"}}},
      "Order": {"type": "object", "required": ["id"], "properties": {"id": {"type": "string", "examples": ["ord_1"]}, "note": {"type": ["string", "null"], "maxLength": 200, "description": "Shown on the <receipt>."}}}
    }
  },
  "paths": {
    "/v1/shops/{shopId}/orders": {
      "get": {"operationId": "list-orders", "summary": "List orders", "tags": ["Orders"],
        "parameters": [{"name": "shopId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "OK"}}},
      "post": {"operationId": "create-order", "summary": "Create an order", "description": "Places an order in ` + "`shop`" + `.", "tags": ["Orders"],
        "security": [{"bearer": []}],
        "parameters": [{"name": "shopId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Order"}}}},
        "responses": {
          "201": {"description": "Created", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Order"}}}},
          "409": {"description": "Conflict", "content": {"application/problem+json": {"schema": {"$ref": "#/components/schemas/Problem"}}}}
        }}
    }
  }
}`

func build(t *testing.T, opts reference.Options) *reference.Reference {
	t.Helper()
	ref, err := reference.Build([]byte(shopSpec), opts)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestBuild(t *testing.T) {
	ref := build(t, reference.Options{})
	var urls []string
	for _, p := range ref.Pages {
		urls = append(urls, p.URL)
	}
	if want := []string{"/docs", "/docs/orders/list-orders", "/docs/orders/create-order"}; strings.Join(urls, " ") != strings.Join(want, " ") {
		t.Fatalf("page URLs = %v, want %v", urls, want)
	}
	if ref.Title != "Shop API" || ref.Version != "1.2.0" {
		t.Errorf("Title, Version = %q, %q", ref.Title, ref.Version)
	}

	overview := string(ref.Pages[0].Main)
	for _, want := range []string{"<strong>things</strong>", "&lt;script&gt;", `<a href="https://example.com/guide">the guide</a>`, `href="/docs/orders/create-order"`, `id="errors"`} {
		if !strings.Contains(overview, want) {
			t.Errorf("overview lacks %s", want)
		}
	}
	if strings.Contains(overview, "<script>") {
		t.Error("overview renders a <script> from the description")
	}

	create := ref.Pages[2]
	for _, want := range []string{`/v1/shops/<i>{shopId}</i>/orders`, "Try it", "&lt;receipt&gt;", "maxLength: 200", "nullable", "<code>shop</code>", "Conflict"} {
		if !strings.Contains(string(create.Main), want) {
			t.Errorf("create-order page lacks %s", want)
		}
	}
	for _, want := range []string{"curl", "MethodPost", "ord_1", "409", `data-path="/v1/shops/{shopId}/orders"`, `name="server"`} {
		if !strings.Contains(string(create.Panel), want) {
			t.Errorf("create-order panel lacks %s", want)
		}
	}
	if !strings.Contains(create.Markdown, "`POST /v1/shops/{shopId}/orders`") || create.Method != "POST" || create.Tag != "Orders" {
		t.Errorf("create-order page = %+v", create)
	}
}

func TestSameOriginAndTrailingSlash(t *testing.T) {
	ref := build(t, reference.Options{Title: "API reference", BasePath: "/api-reference/", TrailingSlash: true, SameOrigin: true})
	if ref.Pages[0].URL != "/api-reference/" || ref.Pages[2].URL != "/api-reference/orders/create-order/" {
		t.Errorf("URLs = %s, %s", ref.Pages[0].URL, ref.Pages[2].URL)
	}
	panel := string(ref.Pages[2].Panel)
	if strings.Contains(panel, `name="server"`) || !strings.Contains(panel, "data-same-origin") {
		t.Error("a same-origin reference asks for a server")
	}
}

func TestHandler(t *testing.T) {
	h, err := build(t, reference.Options{SameOrigin: true, SpecURL: "/openapi.json"}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	rec := get("/docs")
	page := rec.Body.String()
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Security-Policy") != reference.ContentSecurityPolicy {
		t.Fatalf("GET /docs = %d, CSP %q", rec.Code, rec.Header().Get("Content-Security-Policy"))
	}
	if strings.Contains(reference.ContentSecurityPolicy, "unsafe") || strings.Contains(page, "https://fonts") || strings.Contains(page, "style=") {
		t.Error("pages must need no inline styles, unsafe policies or external requests")
	}
	for _, path := range []string{"/docs/orders/create-order", "/docs/orders/create-order/"} {
		if rec := get(path); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Create an order") {
			t.Errorf("GET %s = %d", path, rec.Code)
		}
	}

	css := regexp.MustCompile(`href="(/docs/assets/reference\.[0-9a-f]{8}\.css)"`).FindStringSubmatch(page)
	if css == nil {
		t.Fatal("the overview doesn't link the stylesheet")
	}
	rec = get(css[1])
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("GET stylesheet = %d %v", rec.Code, rec.Header())
	}
	for _, font := range regexp.MustCompile(`url\("(fonts/[^"]+)"\)`).FindAllStringSubmatch(rec.Body.String(), -1) {
		if r := get("/docs/assets/" + font[1]); r.Code != http.StatusOK || r.Header().Get("Content-Type") != "font/woff2" {
			t.Errorf("GET %s = %d %s", font[1], r.Code, r.Header().Get("Content-Type"))
		}
	}

	var index []map[string]any
	if rec := get("/docs/search.json"); json.Unmarshal(rec.Body.Bytes(), &index) != nil || len(index) != 3 {
		t.Errorf("search.json = %s", rec.Body.String())
	}
	if rec := get("/docs/orders/create-order.md"); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "# Create an order") {
		t.Errorf("Markdown copy = %d %q", rec.Code, rec.Body.String())
	}
	if rec := get("/docs/nope"); rec.Code != http.StatusNotFound || rec.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("GET /docs/nope = %d, want a 404 page with the policy", rec.Code)
	}
}

func TestRejectsInvalidDocuments(t *testing.T) {
	if _, err := reference.Build([]byte(`{"paths": {"/x": {"get": "nope"}}}`), reference.Options{}); err == nil {
		t.Error("Build() with a malformed operation succeeded")
	}
	if _, err := reference.Build([]byte(`not json`), reference.Options{}); err == nil {
		t.Error("Build() with invalid JSON succeeded")
	}
}
