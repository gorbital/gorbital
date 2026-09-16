package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// stabilityLine is the marker every public package doc carries (ADR-0015,
// ADR-0054).
var stabilityLine = regexp.MustCompile(`(?m)^Stability: (stable|experimental)\b`)

// notLibrary are the top-level directories of the repository that hold no
// library packages: the CLI, apps, prototypes and tooling are separate Go
// modules with no compatibility promise.
var notLibrary = []string{"cli", "docs", "examples", "scripts", "spikes"}

// libraryPackageDirs returns the directories under root holding a public
// library package: Go files outside internal/ and testdata/ in the root
// module and every module under modules/.
func libraryPackageDirs(t *testing.T, root string) []string {
	t.Helper()
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			name := d.Name()
			switch {
			case rel == ".":
				return nil
			case strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "internal" || name == "testdata" || name == "node_modules":
				return filepath.SkipDir
			case !strings.Contains(rel, "/") && slices.Contains(notLibrary, rel):
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			dir := filepath.Dir(path)
			if len(dirs) == 0 || dirs[len(dirs)-1] != dir {
				dirs = append(dirs, dir)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return slices.Compact(dirs)
}

// TestPackagesDeclareStability fails when a public package of the root
// module or of a module under modules/ has no "Stability: stable" or
// "Stability: experimental" line in its package documentation.
func TestPackagesDeclareStability(t *testing.T) {
	root := filepath.Join("..", "..")
	dirs := libraryPackageDirs(t, root)
	if len(dirs) < 20 {
		t.Fatalf("found %d library packages; the walk is broken", len(dirs))
	}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		var name string
		var doc strings.Builder
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.PackageClauseOnly|parser.ParseComments)
			if err != nil {
				t.Fatalf("parse %s: %v", file, err)
			}
			name = f.Name.Name
			if f.Doc != nil {
				doc.WriteString(f.Doc.Text())
			}
		}
		rel, _ := filepath.Rel(root, dir)
		if name != "main" && !stabilityLine.MatchString(doc.String()) {
			t.Errorf("package %s (%s) has no \"Stability: stable\" or \"Stability: experimental\" line in its package doc (ADR-0054)", name, filepath.ToSlash(rel))
		}
	}
}
