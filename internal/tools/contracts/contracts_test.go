package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/modules/openapi"
)

// frozen is the fixture set every later version is compared with. A new set
// comes only with an ADR that accepts the break.
const frozen = "v0.1.0"

// repo is the repository root, relative to this package.
var repo = filepath.Join("..", "..", "..")

// builtIn lists, per golden app, the path prefixes of the endpoints gorbital
// provides (roadmap goal 5: their HTTP contract doesn't change) and the
// prefixes of example code under them that apps own.
var builtIn = []struct {
	app      string
	prefixes []string
	owned    []string
}{
	{app: "minimal", prefixes: []string{"/version"}},
	{app: "full-single", prefixes: []string{"/version", "/v1/auth/", "/ops/", "/v1/flags", "/v1/webhooks/resend"}},
	{
		app:      "full-multi",
		prefixes: []string{"/version", "/v1/auth/", "/ops/", "/v1/flags", "/v1/webhooks/resend", "/v1/orgs", "/v1/invitations/"},
		// The example resource, like /v1/projects in full-single.
		owned: []string{"/v1/orgs/{orgId}/projects"},
	},
}

// TestOpenAPICompatibleWithV010 fails when a built-in operation of a golden
// app breaks a client written against the v0.1.0 OpenAPI document: a removed
// operation, parameter, response status or field, a changed type, a request
// field made required, or authentication added. See openapi.CheckCompatible.
func TestOpenAPICompatibleWithV010(t *testing.T) {
	for _, tc := range builtIn {
		t.Run(tc.app, func(t *testing.T) {
			rel := filepath.Join("examples", tc.app, "api", "openapi.json")
			baseline := withoutPaths(t, read(t, filepath.Join(fixtures(), rel)), tc.owned)
			current := read(t, filepath.Join(repo, rel))
			for _, prefix := range tc.prefixes {
				found, err := openapi.CheckCompatible(baseline, current, prefix)
				if err != nil {
					t.Fatal(err)
				}
				if !hasPath(t, baseline, prefix) {
					t.Errorf("no operation under %s in the %s fixture; fix the prefix", prefix, frozen)
				}
				for _, f := range found {
					t.Errorf("%s: breaking change to %s* compared with %s: %s", rel, prefix, frozen, f)
				}
			}
		})
	}
}

// droppedExamples are names of the v0.1.0 golden apps' own example code that
// the golden apps on gorbital.Main no longer have, with why. Built-in names
// are never dropped.
var droppedExamples = map[string]string{
	"jobs heartbeat": "the example job of v0.1's internal/app; apps on gorbital.Main define jobs in their modules (Phase 9)",
}

// TestSurfaceKeepsV010Names fails when a name recorded in a Full golden app's
// api/surface.json at v0.1.0 (an error code, audit action, permission, role,
// setting key, job name or flag) no longer exists. New names pass.
//
// Since Phase 9 a golden app's api/surface.json records only the app's own
// names (ADR-0083); the built-in ones are the library's, listed in
// docs/reference, which refdocs generates from the running golden apps and
// the source of every gorbital package they link. A v0.1.0 name passes when
// either has it. gorbital/internal/integration checks the running app's
// names against the same fixtures.
func TestSurfaceKeepsV010Names(t *testing.T) {
	reference := referenceNames(t)
	for _, app := range []string{"full-single", "full-multi"} {
		t.Run(app, func(t *testing.T) {
			rel := filepath.Join("examples", app, "api", "surface.json")
			old := surfaceNames(t, read(t, filepath.Join(fixtures(), rel)))
			current := surfaceNames(t, read(t, filepath.Join(repo, rel)))
			if len(old) == 0 {
				t.Fatalf("no names in the %s fixture %s", frozen, rel)
			}
			for _, name := range old {
				_, bare, _ := strings.Cut(name, " ")
				if _, ok := slices.BinarySearch(current, name); ok || reference[bare] || droppedExamples[name] != "" {
					continue
				}
				t.Errorf("%s: %s is in %s but neither the app's surface nor docs/reference has it", rel, name, frozen)
			}
		})
	}
}

// referenceNames returns the names docs/reference lists: the first column
// of each table, in backticks.
func referenceNames(t *testing.T) map[string]bool {
	t.Helper()
	pages, err := filepath.Glob(filepath.Join(repo, "docs", "reference", "*.md"))
	if err != nil || len(pages) < 5 {
		t.Fatalf("docs/reference pages = %v, %v", pages, err)
	}
	names := map[string]bool{}
	for _, page := range pages {
		for line := range strings.Lines(string(read(t, page))) {
			if rest, ok := strings.CutPrefix(line, "| `"); ok {
				if name, _, ok := strings.Cut(rest, "`"); ok {
					names[name] = true
				}
			}
		}
	}
	return names
}

// surfaceNames flattens a surface.json into sorted "kind name" lines, such as
// "error_codes invalid_credentials" or "permissions.org projects.project.read".
func surfaceNames(t *testing.T, data []byte) []string {
	t.Helper()
	var kinds map[string]json.RawMessage
	if err := json.Unmarshal(data, &kinds); err != nil {
		t.Fatal(err)
	}
	var names []string
	for kind, raw := range kinds {
		var list []string
		if err := json.Unmarshal(raw, &list); err == nil {
			for _, name := range list {
				names = append(names, kind+" "+name)
			}
			continue
		}
		var scoped map[string][]string
		if err := json.Unmarshal(raw, &scoped); err != nil {
			t.Fatalf("surface.json %s: want a list of names or scopes of names: %v", kind, err)
		}
		for scope, list := range scoped {
			for _, name := range list {
				names = append(names, kind+"."+scope+" "+name)
			}
		}
	}
	slices.Sort(names)
	return names
}

// withoutPaths removes the operations under the given prefixes from an
// OpenAPI document.
func withoutPaths(t *testing.T, doc []byte, prefixes []string) []byte {
	t.Helper()
	if len(prefixes) == 0 {
		return doc
	}
	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatal(err)
	}
	paths, _ := root["paths"].(map[string]any)
	for path := range paths {
		for _, prefix := range prefixes {
			if strings.HasPrefix(path, prefix) {
				delete(paths, path)
			}
		}
	}
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// hasPath reports whether doc has a path starting with prefix, so a mistyped
// prefix can't pass by comparing nothing.
func hasPath(t *testing.T, doc []byte, prefix string) bool {
	t.Helper()
	var root struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatal(err)
	}
	for path := range root.Paths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func fixtures() string { return filepath.Join(repo, "internal", "contracts", frozen) }

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
