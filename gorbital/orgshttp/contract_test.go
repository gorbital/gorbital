package orgshttp

import (
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
)

// frozenOpenAPI is the OpenAPI document of the golden multi-tenant app at
// v0.2.1, frozen in internal/contracts (ADR-0083).
const frozenOpenAPI = "../../internal/contracts/v0.2.1/examples/full-multi/api/openapi.json"

// scopePrefixes are the paths this module and sign-in's organisation
// service accounts answer on.
var scopePrefixes = []string{"/v1/orgs", "/v1/invitations"}

// appOwnedPaths are the golden app's own example resource, which is not
// the library's: full-multi has projects in the organisation, and the
// test app has its own (testProjects).
var appOwnedPaths = []string{"/v1/orgs/{orgId}/projects"}

// TestOpenAPIIsFrozenAtV021 is the release gate of ADR-0088: putting
// organisations on the scope contract must not change one byte of their
// HTTP contract. It compares every operation under /v1/orgs and
// /v1/invitations, the golden app's own projects aside, with the frozen
// v0.2.1 document. Both sides are re-encoded from JSON, so object keys
// are in one order and nothing but the content is compared.
func TestOpenAPIIsFrozenAtV021(t *testing.T) {
	a := newApp(t, nil)
	spec := do(t, a.Handler(), "GET", "/openapi.json", "")
	if spec.code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d %s", spec.code, spec.body)
	}
	frozen := scopePaths(t, []byte(readFile(t, frozenOpenAPI)))
	current := scopePaths(t, []byte(spec.body))

	for _, path := range slices.Sorted(maps.Keys(frozen)) {
		if _, ok := current[path]; !ok {
			t.Errorf("%s is in the v0.2.1 document and not in this app's", path)
			continue
		}
		if want, got := canonical(t, frozen[path]), canonical(t, current[path]); want != got {
			t.Errorf("%s differs from v0.2.1:\nv0.2.1:\n%s\nnow:\n%s", path, want, got)
		}
	}
	for _, path := range slices.Sorted(maps.Keys(current)) {
		if _, ok := frozen[path]; !ok {
			t.Errorf("%s is new under %v; the v0.2.1 contract is frozen", path, scopePrefixes)
		}
	}
}

// scopePaths returns the paths of an OpenAPI document under the module's
// prefixes, without the golden app's own resource.
func scopePaths(t *testing.T, doc []byte) map[string]any {
	t.Helper()
	var parsed struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		t.Fatal(err)
	}
	out := map[string]any{}
	for path, item := range parsed.Paths {
		owned := slices.ContainsFunc(appOwnedPaths, func(p string) bool { return strings.HasPrefix(path, p) })
		under := slices.ContainsFunc(scopePrefixes, func(p string) bool { return strings.HasPrefix(path, p) })
		if under && !owned {
			out[path] = item
		}
	}
	if len(out) == 0 {
		t.Fatalf("no paths under %v", scopePrefixes)
	}
	return out
}

// canonical encodes a path item with its keys in one order, so the
// comparison is of the content and reads as a diff when it fails.
func canonical(t *testing.T, item any) string {
	t.Helper()
	b, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// readFile reads a file the test compares with.
func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
