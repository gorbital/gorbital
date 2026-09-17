package cli

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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

	code, out, errOut := runOrb(t, "routes")
	if code != 0 || *calls != 1 {
		t.Fatalf("orb routes = %d (%d exports) %s %s", code, *calls, out, errOut)
	}
	golden := filepath.Join(repoRoot(t), "cli", "internal", "cli", "testdata", "routes", "shelfie.txt")
	if *updateJSON {
		writeFile(t, golden, out)
	}
	if want := readFile(t, golden); out != want {
		t.Errorf("orb routes:\n%s\nwant (%s, rewrite with -update):\n%s", out, golden, want)
	}

	code, out, _ = runOrb(t, "routes", "--json", "--module", "books", "--openapi", "api/openapi.json", "--no-input")
	var list routes.List
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || list.Source != routes.SourceFile || list.Total != 5 || *calls != 1 ||
		list.Routes[1].Source.String() != "internal/modules/books/delivery/routes.go:28" || list.Routes[1].Guards[2] != "rate_limit:30/1m0s" {
		t.Errorf("orb routes --json --module books --openapi = %d %s", code, out)
	}
	code, out, _ = runOrb(t, "routes", "--public", "--json")
	if code != 0 || json.Unmarshal([]byte(out), &list) != nil || list.Total != 1 || list.Routes[0].Path != "/version" {
		t.Errorf("orb routes --public --json = %d %s", code, out)
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
