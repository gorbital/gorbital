package cli

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
	"gorbital.dev/cli/internal/routes"
)

// TestExplainRoute checks that orb explain route joins what orb routes
// knows about a route with the module files behind it: the permission its
// guard carries, the scope its records belong to, and where to read next.
func TestExplainRoute(t *testing.T) {
	newMainApp(t, false)
	if code, _, errOut := runOrb(t, append(shelvesArgs, "--allow-dirty", "--no-input")...); code != 0 {
		t.Fatal(errOut)
	}
	fakeRoutesExport(t, nil)

	// A --scope user route: its records are the signed-in user's, so no
	// tenant column and no membership guard.
	code, out, errOut := runOrb(t, "explain", "route", "get", "/v1/shelves", "--json")
	var e routeExplanation
	if code != 0 || json.Unmarshal([]byte(out), &e) != nil {
		t.Fatalf("orb explain route get /v1/shelves --json = %d %s %s", code, out, errOut)
	}
	if e.Route.Method != "GET" || e.Route.Module != "shelves" || e.SignIn != "required" ||
		e.Route.Scope != recipes.ScopeUser || e.ScopeColumn != "" || e.Membership {
		t.Errorf("explain GET /v1/shelves = %+v", e)
	}
	if e.Route.Source == nil || !strings.HasPrefix(e.Route.Source.File, "internal/modules/shelves/") {
		t.Errorf("explain GET /v1/shelves source = %v", e.Route.Source)
	}
	// The files are the ones the reader hasn't been given: the repository,
	// not the delivery file Code already names.
	if !slices.ContainsFunc(e.Files.Repository, func(p string) bool {
		return strings.HasPrefix(p, "internal/modules/shelves/repository/")
	}) {
		t.Errorf("explain GET /v1/shelves repository = %v", e.Files.Repository)
	}
	if slices.ContainsFunc(e.Files.Repository, func(p string) bool { return strings.Contains(p, "/delivery/") }) {
		t.Errorf("explain GET /v1/shelves lists delivery files twice: %v", e.Files.Repository)
	}

	// The method is matched case-insensitively, and the text form has the
	// sections a reader walks in order.
	code, out, errOut = runOrb(t, "explain", "route", "GET", "/v1/shelves")
	for _, want := range []string{"GET /v1/shelves", "Access", "sign-in", "Request", "Code", "registered at", "internal/modules/shelves/repository/"} {
		if !strings.Contains(out, want) {
			t.Errorf("orb explain route GET /v1/shelves (%d) has no %q:\n%s\n%s", code, want, out, errOut)
		}
	}

	// A public route needs no sign-in, and says so rather than leaving the
	// reader to infer it from an empty guard list.
	code, out, errOut = runOrb(t, "explain", "route", "POST", "/v1/phone-sign-in", "--json")
	var pub routeExplanation
	if code != 0 || json.Unmarshal([]byte(out), &pub) != nil {
		t.Fatalf("orb explain route POST /v1/phone-sign-in --json = %d %s %s", code, out, errOut)
	}
	if pub.SignIn != "public" || !pub.Route.Public {
		t.Errorf("explain POST /v1/phone-sign-in sign-in = %q (public %v)", pub.SignIn, pub.Route.Public)
	}
}

// TestExplainRouteFiles checks that a --scope custom module's policy.go is
// named, since it is what decides access to the module's records, and that
// a module's own tests are told from another module's.
func TestExplainRouteFiles(t *testing.T) {
	dir := newMainApp(t, false)
	if code, _, errOut := runOrb(t, "gen", "module", "Ticket", "subject:string:unique", "--scope", "custom", "--allow-dirty", "--no-input"); code != 0 {
		t.Fatal(errOut)
	}
	files := readModuleFiles(dir, "tickets", &routes.Pos{File: "internal/modules/tickets/delivery/routes.go", Line: 1})
	if files.Policy != "internal/modules/tickets/policy.go" {
		t.Errorf("tickets policy = %q, want internal/modules/tickets/policy.go", files.Policy)
	}
	if !slices.ContainsFunc(files.Tests, func(p string) bool { return strings.HasSuffix(p, "policy_test.go") }) {
		t.Errorf("tickets tests = %v, want policy_test.go among them", files.Tests)
	}
	for _, p := range slices.Concat(files.Repository, files.Tests) {
		if !strings.HasPrefix(p, filepath.ToSlash("internal/modules/tickets/")) {
			t.Errorf("tickets files name another module: %s", p)
		}
	}
	// A module with no directory of its own, as a library module's routes
	// have, contributes no files rather than failing.
	if got := readModuleFiles(dir, "authhttp", nil); got.Policy != "" || len(got.Repository) != 0 || len(got.Tests) != 0 {
		t.Errorf("readModuleFiles for a library module = %+v", got)
	}
}

// TestExplainRouteNotFound checks that a path that isn't a route fails with
// the routes it is close to, and a path served by another method says which.
func TestExplainRouteNotFound(t *testing.T) {
	newMainApp(t, false)
	if code, _, errOut := runOrb(t, append(shelvesArgs, "--allow-dirty", "--no-input")...); code != 0 {
		t.Fatal(errOut)
	}
	fakeRoutesExport(t, nil)

	code, _, errOut := runOrb(t, "explain", "route", "GET", "/v1/shelf")
	if code != 1 || !strings.Contains(errOut, "did you mean") || !strings.Contains(errOut, "/v1/shelves") {
		t.Errorf("orb explain route GET /v1/shelf = %d %s", code, errOut)
	}

	code, _, errOut = runOrb(t, "explain", "route", "PUT", "/v1/shelves")
	if code != 1 || !strings.Contains(errOut, "this path serves") || !strings.Contains(errOut, "GET") {
		t.Errorf("orb explain route PUT /v1/shelves = %d %s", code, errOut)
	}

	// A subject orb doesn't explain, a missing one and a half-written one
	// are usage errors (exit 2), not failures.
	for _, args := range [][]string{
		{"explain", "migration"},
		{"explain"},
		{"explain", "route", "GET"},
	} {
		if code, _, _ := runOrb(t, args...); code != 2 {
			t.Errorf("orb %s = %d, want 2", strings.Join(args, " "), code)
		}
	}
}

// TestExplainPermission checks that a permission is traced to the routes
// whose guards require it and to where the app names it, and that an
// unknown name is an answer rather than a failure.
func TestExplainPermission(t *testing.T) {
	newMainApp(t, false)
	if code, _, errOut := runOrb(t, append(shelvesArgs, "--allow-dirty", "--no-input")...); code != 0 {
		t.Fatal(errOut)
	}
	fakeRoutesExport(t, nil)

	code, out, errOut := runOrb(t, "explain", "route", "GET", "/v1/shelves", "--json")
	var route routeExplanation
	if code != 0 || json.Unmarshal([]byte(out), &route) != nil || len(route.Permissions) == 0 {
		t.Fatalf("orb explain route GET /v1/shelves --json = %d %s %s", code, out, errOut)
	}
	name := route.Permissions[0]

	code, out, errOut = runOrb(t, "explain", "permission", name, "--json")
	var p permissionExplanation
	if code != 0 || json.Unmarshal([]byte(out), &p) != nil {
		t.Fatalf("orb explain permission %s --json = %d %s %s", name, code, out, errOut)
	}
	if p.Permission != name || len(p.DeclaredAt) == 0 ||
		!slices.ContainsFunc(p.Routes, func(r routes.Route) bool { return r.Path == "/v1/shelves" }) {
		t.Errorf("explain permission %s = %+v", name, p)
	}

	// An unknown permission: no route requires it, and the notes say so.
	var none permissionExplanation
	code, out, _ = runOrb(t, "explain", "permission", "nothing.at.all", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &none) != nil || len(none.Routes) != 0 || len(none.Notes) == 0 {
		t.Errorf("orb explain permission nothing.at.all --json = %d %s", code, out)
	}
}

// TestExplainScope checks that orb explain scope reports the app's tenant
// vocabulary and the rule each generated module was created with, and
// refuses a module gorbital.yaml doesn't record.
func TestExplainScope(t *testing.T) {
	newMainApp(t, false)
	for _, args := range [][]string{
		append(shelvesArgs, "--allow-dirty"),
		{"gen", "module", "Ticket", "subject:string:unique", "--scope", "custom", "--allow-dirty"},
	} {
		if code, _, errOut := runOrb(t, append(args, "--no-input")...); code != 0 {
			t.Fatalf("orb %s = %d %s", strings.Join(args, " "), code, errOut)
		}
	}

	var all scopeExplanation
	code, out, errOut := runOrb(t, "explain", "scope", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &all) != nil {
		t.Fatalf("orb explain scope --json = %d %s %s", code, out, errOut)
	}
	if all.Tenant.Name == "" || all.Tenant.Column == "" ||
		all.Modules["shelves"] != recipes.ScopeUser || all.Modules["tickets"] != recipes.ScopeCustom {
		t.Errorf("explain scope = %+v", all)
	}

	// One module narrows the table to it. The result goes in a fresh value:
	// encoding/json merges into a map it finds already populated.
	var one scopeExplanation
	code, out, errOut = runOrb(t, "explain", "scope", "tickets", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &one) != nil || len(one.Modules) != 1 || one.Modules["tickets"] != recipes.ScopeCustom {
		t.Errorf("orb explain scope tickets --json = %d %s %s", code, out, errOut)
	}

	code, out, errOut = runOrb(t, "explain", "scope")
	if code != 0 || !strings.Contains(out, all.Tenant.Name) || !strings.Contains(out, "tickets") {
		t.Errorf("orb explain scope = %d %s %s", code, out, errOut)
	}

	if code, _, errOut = runOrb(t, "explain", "scope", "nothing"); code != 1 || !strings.Contains(errOut, "no module") {
		t.Errorf("orb explain scope nothing = %d %s", code, errOut)
	}
}

// TestNearSegment checks the heuristic behind "did you mean": a segment is
// near another when they share a start long enough not to be a coincidence,
// so a plural finds its singular without every word finding every other.
func TestNearSegment(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"shelves", "shelf", true}, // the plural isn't the singular's prefix
		{"books", "book", true},    // and here it is
		{"projects", "project", true},
		{"shelves", "shelves", true},
		{"profiles", "projects", false}, // three shared characters is a coincidence
		{"settings", "sessions", false},
		{"users", "shelves", false},
		{"id", "id", true}, // too short to compare by prefix: equal or nothing
		{"id", "ids", false},
	} {
		if got := nearSegment(c.a, c.b); got != c.want {
			t.Errorf("nearSegment(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// TestPathSegments checks that parameters and version segments are left out
// of the comparison: every route shares /v1, and {id} matches anything.
func TestPathSegments(t *testing.T) {
	for path, want := range map[string][]string{
		"/v1/orgs/{orgId}/projects": {"orgs", "projects"},
		"/v1/shelves/{id}":          {"shelves"},
		"/v2/books":                 {"books"},
		"/version":                  {"version"},
		"/":                         nil,
	} {
		if got := pathSegments(path); !slices.Equal(got, want) {
			t.Errorf("pathSegments(%q) = %v, want %v", path, got, want)
		}
	}
}
