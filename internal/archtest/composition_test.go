package archtest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModulesDontRequireComposition fails when a module under modules/
// requires the composition module gorbital.dev/gorbital: modules sit below the
// layer that composes them (ADR-0019, ADR-0081), and a built-in module that
// needs gorbital.Module belongs in gorbital/ instead (ADR-0083).
func TestModulesDontRequireComposition(t *testing.T) {
	root := filepath.Join("..", "..")
	found := 0
	err := filepath.WalkDir(filepath.Join(root, "modules"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.Name() != "go.mod" {
			return nil
		}
		found++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for line := range strings.Lines(string(data)) {
			fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "require "))
			if len(fields) > 0 && fields[0] == "gorbital.dev/gorbital" {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s requires gorbital.dev/gorbital; modules must not depend on the composition layer (ADR-0081)", filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if found < 10 {
		t.Fatalf("found %d module go.mod files; the walk is broken", found)
	}
}
