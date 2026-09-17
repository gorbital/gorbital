//orb:noeject reads every Go file of the gorbital repository

package orgshttp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// repo is the gorbital repository's root.
var repo = filepath.Join("..", "..")

// TestRowLevelSecurityBypassesAreKnown lists every call to
// postgres.WithoutRowLevelSecurity outside tests in the repository: each is
// a system path reviewed in ADR-0061 or ADR-0083, and the first connection
// each acquires is logged with its reason (modules/postgres
// TestRowLevelSecurityFollowsTheContext). Request paths, guard.OrgMember and the
// organisations module never bypass the policies.
func TestRowLevelSecurityBypassesAreKnown(t *testing.T) {
	allowed := map[string]int{
		"modules/postgres/migrate.go": 2, // migrations run across organisations
	}
	found := map[string]int{}
	err := filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) && path != repo:
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		}
		src, err := os.ReadFile(path) //nolint:gosec // the repository's own files
		if err != nil || !strings.Contains(string(src), "WithoutRowLevelSecurity") {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
		if err != nil {
			return nil //nolint:nilerr // templates and broken fixtures aren't code
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				if fun.Sel.Name == "WithoutRowLevelSecurity" {
					rel, _ := filepath.Rel(repo, path)
					found[filepath.ToSlash(rel)]++
				}
			case *ast.Ident:
				if fun.Name == "WithoutRowLevelSecurity" {
					rel, _ := filepath.Rel(repo, path)
					found[filepath.ToSlash(rel)]++
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for file, n := range found {
		if allowed[file] != n {
			t.Errorf("%s calls postgres.WithoutRowLevelSecurity %d times; a bypass needs a review in ADR-0083's Phase 7 threat model and an entry here", file, n)
		}
	}
	for file := range allowed {
		if _, ok := found[file]; !ok {
			t.Errorf("%s no longer bypasses row-level security; remove it from the list", file)
		}
	}
	if keys := slices.Sorted(func(yield func(string) bool) {
		for k := range found {
			if !yield(k) {
				return
			}
		}
	}); len(keys) == 0 {
		t.Error("found no call at all; is the repository root right?")
	}
}
