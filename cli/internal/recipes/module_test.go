package recipes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/imports"
)

const goldenShelfie = "../../../examples/shelfie"

// shelvesData is the golden module's command:
//
//	orb gen module Shelf name:string:unique description:text 'visibility:enum(private,shared)' --plural Shelves
func shelvesData(t *testing.T) ModuleData {
	t.Helper()
	fields, err := ParseModuleFields([]string{"name:string:unique", "description:text", "visibility:enum(private,shared)"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewModuleData("example.com/shelfie", "Shelf", fields, ResourceOptions{Plural: "Shelves", Migration: "20260920000002"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// clubBooksData is the golden organisation module's command:
//
//	orb gen module ClubBook title:string:unique 'author:string?' 'status:enum(proposed,reading,finished)' note:text --org
func clubBooksData(t *testing.T) ModuleData {
	t.Helper()
	fields, err := ParseModuleFields([]string{"title:string:unique", "author:string?", "status:enum(proposed,reading,finished)", "note:text"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewModuleData("example.com/shelfie", "ClubBook", fields, ResourceOptions{Scope: ScopeOrg, Migration: "20260920000005"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// TestModuleMatchesShelfie checks that orb gen module reproduces
// examples/shelfie's shelves module (owned by users), its clubbooks
// module (owned by organisations) and their migrations exactly, and that
// the architecture test it writes into apps without one is Shelfie's
// (ADR-0083). After changing the templates, run
// go test -run TestModuleMatchesShelfie -update and review Shelfie's diff.
func TestModuleMatchesShelfie(t *testing.T) {
	for _, d := range []ModuleData{shelvesData(t), clubBooksData(t)} {
		t.Run(d.Package, func(t *testing.T) { checkGoldenModule(t, goldenShelfie, "example.com/shelfie", d) })
	}
}

// goldenProjects are the golden Full apps on gorbital.Main, whose example
// projects module orb gen module writes (Phase 9):
//
//	orb gen module Project name:string:unique description:text 'status:enum(active,archived)'
//
// owned by users in examples/full-single, and with --org by organisations in
// examples/full-multi. Their migrations keep the versions of the v0.1 golden
// apps' projects migrations: the multi-tenant one runs after the
// organisations module's, so its foreign key to orgs is created.
var goldenProjects = []struct{ dir, scope, migration string }{
	{"../../../examples/full-single", ScopeUser, "20260915000002"},
	{"../../../examples/full-multi", ScopeOrg, "20260916000002"},
}

// TestModuleMatchesGoldenApps checks that orb gen module reproduces the
// golden Full apps' projects modules, their migrations and their
// architecture tests exactly. After changing the templates, run
// go test -run 'TestModuleMatches' -update and review the diff.
func TestModuleMatchesGoldenApps(t *testing.T) {
	for _, golden := range goldenProjects {
		t.Run(golden.scope, func(t *testing.T) {
			fields, err := ParseModuleFields([]string{"name:string:unique", "description:text", "status:enum(active,archived)"})
			if err != nil {
				t.Fatal(err)
			}
			d, err := NewModuleData("example.com/acme-api", "Project", fields, ResourceOptions{Scope: golden.scope, Migration: golden.migration})
			if err != nil {
				t.Fatal(err)
			}
			checkGoldenModule(t, golden.dir, "example.com/acme-api", d)
		})
	}
}

func checkGoldenModule(t *testing.T, golden, module string, d ModuleData) {
	files, err := RenderModule(d)
	if err != nil {
		t.Fatalf("RenderModule() error = %v", err)
	}
	arch, err := RenderArchitectureTest(module)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range append(files, arch) {
		f.Content = importCopies(t, golden, module, f)
		target := filepath.Join(golden, filepath.FromSlash(f.Path))
		if *updateGolden {
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, f.Content, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(target)
		if err != nil {
			t.Errorf("read golden %s: %v", f.Path, err)
			continue
		}
		if string(f.Content) != string(want) {
			t.Errorf("generated %s differs from %s's (run with -update and review the diff)", f.Path, golden)
		}
	}
	// No other file is in the module: a file added to Shelfie's shelves by
	// hand would be one the generator doesn't write.
	written := map[string]bool{}
	for _, f := range files {
		written[f.Path] = true
	}
	err = filepath.WalkDir(filepath.Join(golden, filepath.FromSlash(d.Dir())), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(golden, path)
		if !written[filepath.ToSlash(rel)] {
			t.Errorf("%s's %s isn't written by orb gen module", golden, rel)
		}
		return nil
	})
	if err != nil && !*updateGolden {
		t.Error(err)
	}
}

func TestModuleNames(t *testing.T) {
	title := []Field{{Name: "title", Ident: "Title", Human: "title", Kind: KindString}}
	for _, tt := range []struct {
		name, plural, want string
	}{
		{"OrderItem", "", ""},
		{"Page", "", "clash"},
		{"Domain", "", "clash"},
		{"Item", "", "clash"},
		{"Go", "", "clash"},
		{"Shelf", "Shelves", ""},
	} {
		_, err := NewModuleData("example.com/app", tt.name, title, ResourceOptions{Plural: tt.plural, Migration: "20260101000000"})
		if tt.want == "" && err != nil || tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("NewModuleData(%s) error = %v, want %q", tt.name, err, tt.want)
		}
	}
	d, err := NewModuleData("example.com/app", "Shelf", title, ResourceOptions{Plural: "Shelves", Scope: ScopeOrg, RLS: true, Migration: "20260101000000"})
	if err != nil || !d.Org || !d.RLS || d.RoutePath() != "/v1/orgs/{orgId}/shelves" || d.Guard() != "guard.OrgMember" {
		t.Errorf("NewModuleData(org) = %+v, %v; want an organisation module with row-level security", d, err)
	}
	if d, err := NewModuleData("example.com/app", "Shelf", title, ResourceOptions{Plural: "Shelves", RLS: true, Migration: "20260101000000"}); err != nil || d.Org || d.RLS || d.RoutePath() != "/v1/shelves" {
		t.Errorf("NewModuleData(user, RLS) = %+v, %v; want a user module without row-level security", d, err)
	}
}

func TestParseModuleFields(t *testing.T) {
	fields, err := ParseModuleFields([]string{"name:string", "nickname:string?"})
	if err != nil || len(fields) != 2 || !fields[1].Optional || fields[1].MinLength() != 0 || fields[0].MinLength() != 1 {
		t.Fatalf("ParseModuleFields() = %+v, %v", fields, err)
	}
	for spec, want := range map[string]string{
		"nickname:string?:unique": "can't be unique",
		"notes:text?":             "already optional",
	} {
		if _, err := ParseModuleFields([]string{"name:string", spec}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseModuleFields(%s) error = %v, want %q", spec, err, want)
		}
	}
	if _, err := ParseModuleFields([]string{"nickname:string?"}); err == nil || !strings.Contains(err.Error(), "required string") {
		t.Errorf("ParseModuleFields(only optional) error = %v", err)
	}
	if _, err := ParseFields([]string{"name:string", "nickname:string?"}); err == nil || !strings.Contains(err.Error(), "orb gen module") {
		t.Errorf("ParseFields(optional) error = %v, want a pointer to orb gen module", err)
	}
}

// importCopies points a generated Go file's imports of sign-in and
// organisations at the golden app's copies, when it holds them, as orb gen
// module does in an app whose gorbital.lock records them ejected.
func importCopies(t *testing.T, golden, module string, f JobFile) []byte {
	t.Helper()
	if !strings.HasSuffix(f.Path, ".go") {
		return f.Content
	}
	out, _, err := imports.Rewrite(f.Path, f.Content, func(imp string) (string, string, bool) {
		for _, e := range []EjectedModule{{Name: "auth", Package: "gorbital.dev/gorbital/authhttp"}, {Name: "orgs", Package: "gorbital.dev/gorbital/orgshttp"}} {
			if imp != e.Package {
				continue
			}
			if _, err := os.Stat(filepath.Join(golden, filepath.FromSlash(e.Dir()))); err == nil {
				return module + "/" + e.Dir(), filepath.Base(e.Package), true
			}
		}
		return "", "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
