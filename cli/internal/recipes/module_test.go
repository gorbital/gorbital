package recipes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goldenShelfie = "../../../examples/apps/shelfie"

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

// TestModuleMatchesShelfie checks that orb gen module reproduces
// examples/apps/shelfie's shelves module and its migration exactly, and
// that the architecture test it writes into apps without one is Shelfie's
// (ADR-0083). After changing the templates, run
// go test -run TestModuleMatchesShelfie -update and review Shelfie's diff.
func TestModuleMatchesShelfie(t *testing.T) {
	files, err := RenderModule(shelvesData(t))
	if err != nil {
		t.Fatalf("RenderModule() error = %v", err)
	}
	arch, err := RenderArchitectureTest("example.com/shelfie")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range append(files, arch) {
		target := filepath.Join(goldenShelfie, filepath.FromSlash(f.Path))
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
			t.Errorf("generated %s differs from Shelfie's (run with -update and review the diff)", f.Path)
		}
	}
	// No other file is in the module: a file added to Shelfie's shelves by
	// hand would be one the generator doesn't write.
	written := map[string]bool{}
	for _, f := range files {
		written[f.Path] = true
	}
	err = filepath.WalkDir(filepath.Join(goldenShelfie, "internal", "modules", "shelves"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(goldenShelfie, path)
		if !written[filepath.ToSlash(rel)] {
			t.Errorf("Shelfie's %s isn't written by orb gen module", rel)
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
	if _, err := NewModuleData("example.com/app", "Shelf", title, ResourceOptions{Plural: "Shelves", Scope: ScopeOrg, Migration: "20260101000000"}); err == nil || !strings.Contains(err.Error(), "Phase 7") {
		t.Errorf("NewModuleData(org) error = %v, want the Phase 7 refusal", err)
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
