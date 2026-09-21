package recipes_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"gorbital.dev/cli/internal/recipes"
)

// TestManifestCoversTheTree: every template in the tree has an entry, and
// every entry names a template. A file added without an entry fails here
// and in go generate, not in somebody's app.
func TestManifestCoversTheTree(t *testing.T) {
	if err := recipes.CheckManifest(recipes.Templates(), recipes.TreeV03); err != nil {
		t.Error(err)
	}
}

// TestManifestReportsBothWays: a template with no entry, and an entry with
// no template, are both errors naming the path.
func TestManifestReportsBothWays(t *testing.T) {
	const manifest = "paths:\n  a.tmpl: always\n  gone.tmpl: [auth.full]\n"
	tree := fstest.MapFS{
		"t/" + recipes.ManifestName: {Data: []byte(manifest)},
		"t/a.tmpl":                  {Data: []byte("a")},
		"t/b.tmpl":                  {Data: []byte("b")},
	}
	err := recipes.CheckManifest(tree, "t")
	if err == nil {
		t.Fatal("CheckManifest() = nil, want an error")
	}
	for _, want := range []string{"b.tmpl", "gone.tmpl"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q: %v", want, err)
		}
	}
}

// TestManifestRejectsAnUnknownFeature: a typo in a feature name is an
// error, not a path nothing ever writes.
func TestManifestRejectsAnUnknownFeature(t *testing.T) {
	tree := fstest.MapFS{
		"t/" + recipes.ManifestName: {Data: []byte("paths:\n  a.tmpl: [scope.nammed]\n")},
		"t/a.tmpl":                  {Data: []byte("a")},
	}
	if err := recipes.CheckManifest(tree, "t"); err == nil || !strings.Contains(err.Error(), "scope.nammed") {
		t.Errorf("CheckManifest() = %v, want an error naming the unknown feature", err)
	}
}

// TestRenderWritesWhatTheProfileAsksFor: the paths a profile's features
// turn on, and no others.
func TestRenderWritesWhatTheProfileAsksFor(t *testing.T) {
	cases := []struct {
		auth, scope   string
		want, without []string
	}{
		{recipes.AuthFull, recipes.DefaultScopeName,
			[]string{"cmd/api/main.go", "AUTH_PROVIDERS.md", "db/row_level_security.sql"},
			[]string{"internal/modules/scope/authorizer.go"}},
		{recipes.AuthFull, recipes.ScopeCustom,
			[]string{"AUTH_PROVIDERS.md", "internal/modules/scope/authorizer.go", "internal/modules/scope/scope.go"},
			[]string{"db/row_level_security.sql"}},
		{recipes.AuthBasic, recipes.ScopeNone,
			[]string{"cmd/api/main.go", "gorbital.yaml"},
			[]string{"AUTH_PROVIDERS.md", "db/row_level_security.sql", "internal/modules/scope/authorizer.go"}},
		{recipes.AuthNone, recipes.ScopeNone,
			[]string{"cmd/api/main.go", "compose.yaml"},
			[]string{"AUTH_PROVIDERS.md", "db/row_level_security.sql"}},
	}
	for _, tc := range cases {
		p, err := recipes.ParseProfile(tc.auth, tc.scope)
		if err != nil {
			t.Fatal(err)
		}
		tree, err := recipes.RenderTree(recipes.Templates(), recipes.TreeV03, p, data())
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		for _, path := range tc.want {
			if _, ok := tree[path]; !ok {
				t.Errorf("%s doesn't write %s", p, path)
			}
		}
		for _, path := range tc.without {
			if _, ok := tree[path]; ok {
				t.Errorf("%s writes %s, which it has no use for", p, path)
			}
		}
		// The demonstration module and the API artefacts are produced, not
		// templated (ADR-0090 §5).
		for path := range tree {
			if strings.HasPrefix(path, "internal/modules/projects/") || strings.HasPrefix(path, "api/") {
				t.Errorf("%s writes %s from a template; orb new produces it", p, path)
			}
		}
	}
}

// TestEveryLegalProfileRenders: all nine shapes render, and every one of
// them writes main.go and gorbital.yaml.
func TestEveryLegalProfileRenders(t *testing.T) {
	for _, p := range recipes.LegalProfiles() {
		tree, err := recipes.RenderTree(recipes.Templates(), recipes.TreeV03, p, data())
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		for _, path := range []string{"cmd/api/main.go", "gorbital.yaml", "go.mod", "README.md"} {
			if _, ok := tree[path]; !ok {
				t.Errorf("%s doesn't write %s", p, path)
			}
		}
		if got := string(tree["gorbital.yaml"]); !strings.Contains(got, "auth: "+p.Auth) {
			t.Errorf("%s: gorbital.yaml doesn't record the profile:\n%s", p, got)
		}
	}
}

// TestTreePathCount: the one tree is smaller than the two it replaced.
func TestTreePathCount(t *testing.T) {
	var paths []string
	err := fs.WalkDir(recipes.Templates(), recipes.TreeV03, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) > 64 {
		t.Errorf("%s has %d files; v0.2/full and v0.2/full-multi had 63 and 64 between them, and one tree must be smaller than either", recipes.TreeV03, len(paths))
	}
	if !slices.Contains(paths, recipes.TreeV03+"/"+recipes.ManifestName) {
		t.Errorf("%s has no %s", recipes.TreeV03, recipes.ManifestName)
	}
}

func data() recipes.Data {
	return recipes.Data{Name: "acme-api", Module: "example.com/acme-api", LibraryVersion: recipes.LibraryVersion}
}
