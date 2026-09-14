package recipes_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"apistock.dev/cli/internal/recipes"
	"apistock.dev/cli/internal/recipes/generate"
)

const exampleDir = "../../../examples/minimal"

func renderInto(t *testing.T, d recipes.Data) (string, []recipes.File) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	files, err := recipes.RenderMinimal(root, d)
	if err != nil {
		t.Fatalf("RenderMinimal() error = %v", err)
	}
	return dir, files
}

// TestGoldenMinimal: rendering with the placeholder name reproduces the
// hand-written golden app exactly.
func TestGoldenMinimal(t *testing.T) {
	dir, files := renderInto(t, recipes.Data{
		Name:           generate.PlaceholderName,
		Module:         generate.PlaceholderModule,
		LibraryVersion: recipes.LibraryVersion,
	})

	rendered := map[string]bool{}
	for _, f := range files {
		rendered[f.Path] = true
	}

	err := filepath.WalkDir(exampleDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(exampleDir, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if generate.SkippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if generate.Skipped(rel) {
			return nil
		}
		want, _ := os.ReadFile(p)
		got, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			t.Errorf("golden file %s was not rendered", rel)
			return nil
		}
		if !bytes.Equal(got, want) {
			t.Errorf("rendered %s differs from examples/minimal/%s", rel, rel)
		}
		delete(rendered, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	delete(rendered, "go.mod")
	for extra := range rendered {
		t.Errorf("rendered %s, which is not in examples/minimal", extra)
	}
}

func TestRenderGoMod(t *testing.T) {
	dir, _ := renderInto(t, recipes.Data{Name: "shop-api", Module: "github.com/acme/shop-api", LibraryVersion: "v0.1.0"})
	goMod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.HasPrefix(string(goMod), "module github.com/acme/shop-api\n") || !strings.Contains(string(goMod), "apistock.dev v0.1.0") || strings.Contains(string(goMod), "replace") {
		t.Errorf("go.mod without Local:\n%s", goMod)
	}
	main, _ := os.ReadFile(filepath.Join(dir, "cmd", "api", "main.go"))
	if !strings.Contains(string(main), `"github.com/acme/shop-api/internal/app"`) || strings.Contains(string(main), "acme-api") {
		t.Errorf("cmd/api/main.go not rendered for the new module:\n%s", main)
	}

	dir, _ = renderInto(t, recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: "v0.1.0", Local: "/src/apistock"})
	goMod, _ = os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.Contains(string(goMod), "apistock.dev/modules/openapi => /src/apistock/modules/openapi") {
		t.Errorf("go.mod with Local lacks replace directives:\n%s", goMod)
	}
}

// TestTemplatesUpToDate fails when examples/minimal changed but
// `go generate ./...` wasn't run.
func TestTemplatesUpToDate(t *testing.T) {
	fresh := t.TempDir()
	if err := generate.Run(exampleDir, fresh); err != nil {
		t.Fatalf("generate.Run() error = %v", err)
	}
	compareTrees(t, fresh, "minimal")
	compareTrees(t, "minimal", fresh)
}

func compareTrees(t *testing.T, a, b string) {
	t.Helper()
	_ = filepath.WalkDir(a, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(a, p)
		want, _ := os.ReadFile(p)
		got, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("template %s is out of date; run: go generate ./... (in cli/)", rel)
		}
		return nil
	})
}
