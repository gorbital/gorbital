package modules_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// layerImports are the app packages each layer may import, within its own
// module: domain nothing but the standard library, the use cases their
// domain, the repository and delivery the use cases and the domain, and
// module.go every layer. A module never imports another module's layers;
// it can take another module's root package, as modules that take the
// authenticator of a sign-in module ejected with orb eject do (ADR-0083).
var layerImports = map[string][]string{
	"domain":     {},
	"usecase":    {"domain"},
	"repository": {"domain", "usecase"},
	"delivery":   {"domain", "usecase"},
	"":           {"domain", "usecase", "repository", "delivery"},
}

func TestModuleLayers(t *testing.T) {
	const app = "example.com/plateful/internal/modules/"
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || !strings.Contains(path, string(filepath.Separator)) {
			return err
		}
		parts := strings.Split(filepath.ToSlash(path), "/")
		module, layer := parts[0], ""
		if len(parts) > 2 {
			layer = parts[1]
		}
		allowed, ok := layerImports[layer]
		if !ok {
			t.Errorf("%s: %q isn't a layer: use domain, usecase, repository or delivery", path, layer)
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			imp, _ := strconv.Unquote(spec.Path.Value)
			if layer == "domain" && strings.Contains(strings.SplitN(imp, "/", 2)[0], ".") {
				t.Errorf("%s imports %s: the domain uses the standard library only", path, imp)
			}
			rest, ok := strings.CutPrefix(imp, app)
			if !ok {
				continue
			}
			target := strings.SplitN(rest, "/", 3)
			if target[0] != module {
				if len(target) > 1 {
					t.Errorf("%s imports %s: a module never imports another module's layers", path, imp)
				}
				continue
			}
			if len(target) < 2 || !contains(allowed, target[1]) {
				t.Errorf("%s (layer %q) imports %s", path, layer, imp)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
