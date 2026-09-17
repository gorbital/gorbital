// Package ejectable checks that the built-in modules orb eject copies into
// apps can be copied (ADR-0083, Phase 9): an app can't import gorbital's
// internal packages, so a module's code and the tests orb eject keeps use
// only public API and the module's own packages; every internal package is
// under one of the four layers an app module has, and follows the layer
// rules of an app's architecture test; and the module still compiles, tests
// included, without the test files marked //orb:noeject.
package ejectable

import (
	"bufio"
	"encoding/json"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// modules are the packages orb eject copies, as its command names them.
var modules = []string{"authhttp", "opshttp", "orgshttp", "flagshttp", "mailevents"}

const library = "gorbital.dev/gorbital/"

// noEject is the directive a test file starts with when orb eject leaves it
// out, followed by the reason: tests of the library itself, such as those
// comparing a module with the frozen v0.1.0 contracts in this repository.
const noEject = "//orb:noeject"

// layerImports are the layers each layer may import within its module, as
// in an app's internal/modules/architecture_test.go; "" is the module's
// root package.
var layerImports = map[string][]string{
	"domain":     {},
	"usecase":    {"domain"},
	"repository": {"domain", "usecase"},
	"delivery":   {"domain", "usecase"},
	"":           {"domain", "usecase", "repository", "delivery"},
}

// root returns the gorbital module's directory.
func root(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// noEjectReason returns the reason of path's //orb:noeject directive, and
// whether it has one before its package clause.
func noEjectReason(t *testing.T, path string) (string, bool) {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // the repository's own files
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "package ") {
			return "", false
		}
		if rest, ok := strings.CutPrefix(line, noEject); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

func TestModulesUseOnlyPublicAPI(t *testing.T) {
	for _, module := range modules {
		t.Run(module, func(t *testing.T) {
			dir := filepath.Join(root(t), module)
			err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
					return err
				}
				rel, _ := filepath.Rel(dir, path)
				rel = filepath.ToSlash(rel)
				reason, skipped := noEjectReason(t, path)
				switch {
				case skipped && !strings.HasSuffix(path, "_test.go"):
					t.Errorf("%s/%s: only test files can be left out of an ejected module", module, rel)
				case skipped && reason == "":
					t.Errorf("%s/%s: %s needs a reason", module, rel, noEject)
				case skipped:
					return nil
				}
				file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					return err
				}
				layer := layerOf(t, module, rel)
				for _, spec := range file.Imports {
					imp, _ := strconv.Unquote(spec.Path.Value)
					if layer == "domain" && !strings.HasSuffix(path, "_test.go") && strings.Contains(strings.SplitN(imp, "/", 2)[0], ".") {
						t.Errorf("%s/%s imports %s: the domain uses the standard library only", module, rel, imp)
					}
					rest, ok := strings.CutPrefix(imp, library)
					if !ok {
						continue
					}
					own, inModule := strings.CutPrefix(rest, module+"/internal/")
					switch {
					case inModule:
						target, _, _ := strings.Cut(own, "/")
						if !strings.HasSuffix(path, "_test.go") && !slices.Contains(layerImports[layer], target) {
							t.Errorf("%s/%s (layer %q) imports %s", module, rel, layer, imp)
						}
					case strings.HasPrefix(rest, "internal/") || strings.Contains(rest, "/internal/"):
						t.Errorf("%s/%s imports %s, which an app with the module ejected can't import", module, rel, imp)
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// layerOf returns the layer of the file at rel in module, failing the test
// for an internal package outside the four layers.
func layerOf(t *testing.T, module, rel string) string {
	t.Helper()
	parts := strings.Split(rel, "/")
	if parts[0] != "internal" {
		if len(parts) > 1 {
			t.Errorf("%s/%s: a public package below the module; orb eject copies the module's root package and its internal layers", module, rel)
		}
		return ""
	}
	if len(parts) < 3 {
		t.Errorf("%s/%s: a file directly in internal/", module, rel)
		return ""
	}
	if _, ok := layerImports[parts[1]]; !ok || parts[1] == "" {
		t.Errorf("%s/%s: internal/%s isn't a layer; put the package under domain, usecase, repository or delivery", module, rel, parts[1])
	}
	return parts[1]
}

// TestModulesCompileWithoutLibraryTests builds every module with its tests,
// leaving out the files orb eject leaves out, so an ejected module's tests
// compile.
func TestModulesCompileWithoutLibraryTests(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the modules' tests")
	}
	gorbital := root(t)
	overlay := map[string]string{}
	var patterns []string
	for _, module := range modules {
		patterns = append(patterns, "./"+module+"/...")
		err := filepath.WalkDir(filepath.Join(gorbital, module), func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.HasSuffix(path, "_test.go") {
				if _, skipped := noEjectReason(t, path); skipped {
					overlay[path] = ""
				}
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := json.Marshal(map[string]any{"Replace": overlay})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", append([]string{"test", "-overlay", file, "-count=1", "-run", "^$", "-vet=off"}, patterns...)...)
	cmd.Dir = gorbital
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test without the %d files marked %s: %v\n%s", len(overlay), noEject, err, out)
	}
}
