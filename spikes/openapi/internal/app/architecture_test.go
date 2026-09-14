package app_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestLayerImports enforces that only delivery (and the module root) know
// about HTTP frameworks, and that domain uses only the standard library.
func TestLayerImports(t *testing.T) {
	modules, err := filepath.Glob("../modules/*")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string][]string{
		"domain":     {"github.com/", "net/http", "encoding/json", "apistock.dev/"},
		"usecase":    {"github.com/danielgtaylor/huma", "net/http"},
		"repository": {"github.com/danielgtaylor/huma", "net/http"},
	}
	for _, mod := range modules {
		for layer, banned := range forbidden {
			files, _ := filepath.Glob(filepath.Join(mod, layer, "*.go"))
			for _, f := range files {
				for _, imp := range imports(t, f) {
					for _, b := range banned {
						if strings.HasPrefix(imp, b) {
							t.Errorf("%s imports %q; %s layer must not import %q", f, imp, layer, b)
						}
					}
				}
			}
		}
	}
}

func imports(t *testing.T, path string) []string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, spec := range f.Imports {
		p, _ := strconv.Unquote(spec.Path.Value)
		out = append(out, p)
	}
	return out
}
