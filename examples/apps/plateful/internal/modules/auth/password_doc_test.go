package authhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

// passwordDocPlaces are the request body fields whose description states the
// shortest password accepted, with the wording expected for a minimum of n.
// Together with the registration body of an app with RegisterFields, which
// shares POST /v1/auth/register's entry, they are every place the document
// states the minimum.
var passwordDocPlaces = []struct {
	name, method, path, field string
	want                      func(n int) string
}{
	{"register", http.MethodPost, "/v1/auth/register", "password", atLeastDoc},
	{"reset password", http.MethodPost, "/v1/auth/password/reset", "password", func(n int) string {
		return "The new password, at least " + strconv.Itoa(n) + " characters"
	}},
	{"change password", http.MethodPut, "/v1/auth/password", "new_password", atLeastDoc},
	{"operator creates an account", http.MethodPost, "/ops/auth/users", "password", atLeastDoc},
}

func atLeastDoc(n int) string { return "At least " + strconv.Itoa(n) + " characters" }

// shelfFields are an app's extra registration fields, as RegisterFields
// takes them.
type shelfFields struct {
	Shelf string `json:"shelf" maxLength:"100"`
}

func saveShelf(context.Context, pgx.Tx, NewAccount, shelfFields) error { return nil }

// TestPasswordMinimumInOpenAPI: the documented minimum password length
// follows MinPasswordLength, and without the option it is still v0.1's 12
// characters, which TestOpenAPIMatchesV010 requires byte for byte. The
// registration cases cover both bodies: v0.1's and RegisterBody, the one an
// app with RegisterFields serves.
func TestPasswordMinimumInOpenAPI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   []Option
		want   int
		fields bool // the registration body is RegisterBody, not v0.1's
	}{
		{"default", nil, 12, false},
		{"raised", []Option{MinPasswordLength(14)}, 14, false},
		{"raised to the maximum", []Option{MinPasswordLength(128)}, 128, false},
		{"registration fields", []Option{RegisterFields(saveShelf)}, 12, true},
		{"registration fields with a raised minimum", []Option{MinPasswordLength(14), RegisterFields(saveShelf)}, 14, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := authDocument(t, tc.opts...)
			if _, ok := registerBody(t, doc)["shelf"]; ok != tc.fields {
				t.Fatalf("POST /v1/auth/register has a shelf property: %t, want %t", ok, tc.fields)
			}
			for _, place := range passwordDocPlaces {
				got, _ := property(t, doc, place.method, place.path, place.field)["description"].(string)
				if want := place.want(tc.want); got != want {
					t.Errorf("%s: %s description = %q, want %q", place.name, place.field, got, want)
				}
			}
			// The minimum is documented, not validated by Huma: a short
			// password still reaches the app's 422 weak_password, which
			// names the minimum and runs its PasswordPolicy checks.
			if got := property(t, doc, http.MethodPost, "/v1/auth/register", "password")["minLength"]; got != nil {
				t.Errorf("register password has minLength %v; the password policy answers instead", got)
			}
		})
	}
}

// TestPasswordMinimumIsTheOnlyChange: raising the minimum changes the
// document only in those descriptions, so an app that raises it keeps every
// other byte of v0.1's contract.
func TestPasswordMinimumIsTheOnlyChange(t *testing.T) {
	base, raised := authDocumentJSON(t), authDocumentJSON(t, MinPasswordLength(14))
	for _, place := range passwordDocPlaces {
		base = bytes.ReplaceAll(base, []byte(`"`+place.want(12)+`"`), []byte(`"`+place.want(14)+`"`))
	}
	if !bytes.Equal(base, raised) {
		t.Error("MinPasswordLength(14) changed the OpenAPI document beyond the password descriptions")
	}
}

// authDocument builds the OpenAPI document of an app with sign-in and opts,
// without a database, as `go run ./cmd/api openapi` does, and decodes it.
func authDocument(t *testing.T, opts ...Option) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(authDocumentJSON(t, opts...), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// authDocumentJSON is authDocument's document as written.
func authDocumentJSON(t *testing.T, opts ...Option) []byte {
	t.Helper()
	auth := New(opts...)
	if err := auth.CheckConfig(gorbital.Config{Env: "development"}); err != nil {
		t.Fatalf("CheckConfig() error = %v", err)
	}
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	openapi.InstallErrors(mapper)
	api := openapi.New(http.NewServeMux(), testAppName, "test", openapi.WithBearerAuth("a session token"))
	if err := gorbital.Mount(api, mapper, gorbital.Deps{}, auth.Module()); err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	var out bytes.Buffer
	if err := openapi.WriteSpec(&out, api); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// registerBody is POST /v1/auth/register's request body properties.
func registerBody(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	return properties(t, doc, http.MethodPost, "/v1/auth/register")
}

// property returns one of properties' entries.
func property(t *testing.T, doc map[string]any, method, path, field string) map[string]any {
	t.Helper()
	p, ok := properties(t, doc, method, path)[field].(map[string]any)
	if !ok {
		t.Fatalf("%s %s has no %q property", method, path, field)
	}
	return p
}

// properties are the properties of an operation's JSON request body schema,
// following the schema reference as a client would.
func properties(t *testing.T, doc map[string]any, method, path string) map[string]any {
	t.Helper()
	paths, _ := doc["paths"].(map[string]any)
	item, _ := paths[path].(map[string]any)
	op, _ := item[strings.ToLower(method)].(map[string]any)
	if op == nil {
		t.Fatalf("%s %s is not in the document", method, path)
	}
	body, _ := op["requestBody"].(map[string]any)
	content, _ := body["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	schema, _ := media["schema"].(map[string]any)
	if ref, ok := schema["$ref"].(string); ok {
		components, _ := doc["components"].(map[string]any)
		schemas, _ := components["schemas"].(map[string]any)
		schema, _ = schemas[ref[strings.LastIndex(ref, "/")+1:]].(map[string]any)
	}
	props, _ := schema["properties"].(map[string]any)
	if props == nil {
		t.Fatalf("%s %s has no JSON request body schema", method, path)
	}
	return props
}

// TestPasswordMinimumWithoutRegistration: an app that raises the minimum and
// closes registration still documents it on the routes it does serve, and
// registering the routes doesn't fail over the missing one.
func TestPasswordMinimumWithoutRegistration(t *testing.T) {
	doc := authDocument(t, MinPasswordLength(14), WithoutRegistration())
	if paths, _ := doc["paths"].(map[string]any); paths["/v1/auth/register"] != nil {
		t.Fatal("WithoutRegistration still registered POST /v1/auth/register")
	}
	for _, place := range passwordDocPlaces[1:] {
		got, _ := property(t, doc, place.method, place.path, place.field)["description"].(string)
		if want := place.want(14); got != want {
			t.Errorf("%s: %s description = %q, want %q", place.name, place.field, got, want)
		}
	}
}
