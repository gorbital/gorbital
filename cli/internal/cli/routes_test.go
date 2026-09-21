package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/routes"
)

// fakeRoutesExport serves the copy's api/openapi.json as the app's document,
// or fails with err.
func fakeRoutesExport(t *testing.T, err error) *int {
	t.Helper()
	calls := 0
	saved := routesExport
	routesExport = func(_ context.Context, dir string, _ []string) ([]byte, error) {
		calls++
		if err != nil {
			return nil, err
		}
		return []byte(readFile(t, filepath.Join(dir, "api", "openapi.json"))), nil
	}
	t.Cleanup(func() { routesExport = saved })
	return &calls
}

func TestRoutes(t *testing.T) {
	newMainApp(t, false)
	if code, _, errOut := runOrb(t, append(shelvesArgs, "--allow-dirty")...); code != 0 {
		t.Fatal(errOut)
	}
	calls := fakeRoutesExport(t, nil)

	// The library modules' routes (authhttp, opshttp, flagshttp, /version)
	// are listed without a source; the golden is only the app's routes, so it
	// doesn't change when a library module gains a route.
	doc, _, err := routes.FromOpenAPI([]byte(readFile(t, filepath.Join("api", "openapi.json"))))
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runOrb(t, "routes")
	if code != 0 || *calls != 1 || strings.Count(out, "\n") < len(doc) ||
		!strings.Contains(out, fmt.Sprintf("\n%d routes, ", len(doc))) || !strings.Contains(out, "note: orb routes --app lists only the routes in the app's source") {
		t.Fatalf("orb routes = %d (%d exports) %s %s", code, *calls, out, errOut)
	}

	code, out, errOut = runOrb(t, "routes", "--app")
	if code != 0 || *calls != 2 {
		t.Fatalf("orb routes --app = %d (%d exports) %s %s", code, *calls, out, errOut)
	}
	golden := filepath.Join(repoRoot(t), "cli", "internal", "cli", "testdata", "routes", "shelfie.txt")
	if *updateJSON {
		writeFile(t, golden, out)
	}
	if want := readFile(t, golden); out != want {
		t.Errorf("orb routes --app:\n%s\nwant (%s, rewrite with -update):\n%s", out, golden, want)
	}

	code, out, _ = runOrb(t, "routes", "--json", "--module", "books", "--openapi", "api/openapi.json", "--no-input")
	var list routes.List
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || list.Source != routes.SourceFile || list.Total != 7 || *calls != 2 ||
		list.Routes[1].Source.String() != "internal/modules/books/delivery/routes.go:32" || list.Routes[1].Guards[2] != "rate_limit:30/1m0s" {
		t.Errorf("orb routes --json --module books --openapi = %d %s", code, out)
	}

	// Public routes: sign-in's and /version come from the library; the
	// app's own are phone sign-in's two and the partner webhook, which say
	// guard.Public().
	code, out, _ = runOrb(t, "routes", "--public", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || list.Total == 0 || list.Public != list.Total ||
		!slices.ContainsFunc(list.Routes, func(r routes.Route) bool { return r.Path == "/version" && r.Source == nil }) {
		t.Errorf("orb routes --public --json = %d %s", code, out)
	}
	code, out, _ = runOrb(t, "routes", "--public", "--app", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || list.Total != 3 || list.Public != 3 || len(list.Warnings) != 0 ||
		list.Routes[0].Path != "/v1/phone-sign-in" || list.Routes[2].Path != "/v1/webhooks/partners/purchases" {
		t.Errorf("orb routes --public --app --json = %d %s", code, out)
	}
}

// TestRoutesScopes checks the scope column (ADR-0091): every route of a
// generated module says which access rule it serves under, in the app's
// own words, and a route whose module records none says nothing rather
// than guessing from a permission guard.
func TestRoutesScopes(t *testing.T) {
	newMainApp(t, false)
	for _, args := range [][]string{
		append(shelvesArgs, "--allow-dirty"),
		{"gen", "module", "Catalogue", "name:string:unique", "--scope", "public", "--allow-dirty"},
		{"gen", "module", "Ticket", "subject:string:unique", "--scope", "custom", "--allow-dirty"},
	} {
		if code, _, errOut := runOrb(t, append(args, "--no-input")...); code != 0 {
			t.Fatalf("orb %s = %d %s", strings.Join(args, " "), code, errOut)
		}
	}
	fakeRoutesExport(t, nil)

	code, out, errOut := runOrb(t, "routes", "--app", "--json")
	var list routes.List
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil {
		t.Fatalf("orb routes --app --json = %d %s %s", code, out, errOut)
	}
	want := map[string]string{
		"GET /v1/shelves":         "user",
		"POST /v1/shelves":        "user",
		"GET /v1/catalogues":      "public",
		"POST /v1/catalogues":     "public", // the module is public; the route's own guard is the write permission
		"GET /v1/tickets":         "custom",
		"DELETE /v1/tickets/{id}": "custom",
		// clubbooks is Shelfie's organisation module, which this app
		// doesn't generate; profiles is hand-written and records nothing.
		"GET /v1/profile": "",
	}
	for _, r := range list.Routes {
		if scope, checked := want[r.Method+" "+r.Path]; checked && r.Scope != scope {
			t.Errorf("%s %s scope = %q, want %q", r.Method, r.Path, r.Scope, scope)
		}
	}
	if !strings.Contains(readFile(t, manifestPath), "catalogues: public") {
		t.Errorf("gorbital.yaml doesn't record the modules' scopes:\n%s", readFile(t, manifestPath))
	}
}

func TestRoutesErrors(t *testing.T) {
	newMainApp(t, false)
	fakeRoutesExport(t, errors.New("exit status 1: undefined: books"))
	for _, tt := range []struct {
		args []string
		code int
		want string
	}{
		{nil, 1, "couldn't build the OpenAPI document: exit status 1: undefined: books"},
		{[]string{"--openapi", "missing.json"}, 1, "missing.json"},
		{[]string{"--openapi", "go.mod"}, 1, "isn't valid JSON"},
		{[]string{"extra"}, 2, "unexpected arguments"},
		{[]string{"--nope"}, 1, "flag provided but not defined"},
	} {
		code, _, errOut := runOrb(t, append([]string{"routes"}, tt.args...)...)
		if code != tt.code || !strings.Contains(errOut, tt.want) {
			t.Errorf("orb routes %q = %d %q, want %d containing %q", tt.args, code, errOut, tt.code, tt.want)
		}
	}
}
