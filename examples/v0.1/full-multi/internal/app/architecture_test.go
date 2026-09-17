package app_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulesDir = "../modules"

// TestArchitecture enforces the layering rules in ARCHITECTURE.md.
func TestArchitecture(t *testing.T) {
	err := filepath.WalkDir(modulesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, _ := filepath.Rel(modulesDir, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		module, layer := parts[0], ""
		if len(parts) > 2 {
			layer = parts[1]
		}
		checkFile(t, path, module, layer)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func checkFile(t *testing.T, path, module, layer string) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	isTest := strings.HasSuffix(path, "_test.go")

	for _, spec := range file.Imports {
		imp, _ := strconv.Unquote(spec.Path.Value)
		if i := strings.Index(imp, "/internal/modules/"); i >= 0 {
			other := strings.SplitN(imp[i+len("/internal/modules/"):], "/", 2)[0]
			if other != module {
				t.Errorf("%s: module %q imports module %q; modules must not import each other", path, module, other)
			}
		}
		if isTest {
			continue
		}
		switch layer {
		case "domain":
			if strings.Contains(strings.SplitN(imp, "/", 2)[0], ".") {
				t.Errorf("%s: domain imports %q; domain may import only the standard library", path, imp)
			}
		case "usecase":
			if strings.Contains(imp, "/delivery") || strings.Contains(imp, "/repository") || strings.Contains(imp, "danielgtaylor/huma") || imp == "net/http" {
				t.Errorf("%s: usecase imports %q; use cases must not depend on delivery, repository or HTTP", path, imp)
			}
		case "repository":
			if strings.Contains(imp, "/delivery") || strings.Contains(imp, "danielgtaylor/huma") {
				t.Errorf("%s: repository imports %q; repositories must not depend on delivery or HTTP", path, imp)
			}
		case "delivery":
			if strings.Contains(imp, "/repository") {
				t.Errorf("%s: delivery imports %q; delivery must call use cases, not repositories", path, imp)
			}
		}
	}

	if isTest {
		return
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := n.X.(*ast.Ident); ok && id.Name == "os" && (n.Sel.Name == "Getenv" || n.Sel.Name == "LookupEnv" || n.Sel.Name == "Environ") {
				t.Errorf("%s: reads environment variables; only internal/app may", fset.Position(n.Pos()))
			}
		case *ast.StructType:
			if layer != "domain" {
				return true
			}
			for _, f := range n.Fields.List {
				if f.Tag != nil {
					t.Errorf("%s: domain struct field has tag %s; map to API or storage types outside domain", fset.Position(f.Pos()), f.Tag.Value)
				}
			}
		}
		return true
	})
}
