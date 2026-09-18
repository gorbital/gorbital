package generate

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"gorbital.dev/cli/internal/imports"
)

// An Ejected is a built-in module of gorbital.dev/gorbital that a golden app
// holds as its own code, as orb new copies it into every Full app (v0.2.1).
type Ejected struct {
	// Dir is the module's directory in the app, such as
	// internal/modules/auth.
	Dir string
	// Package is the library package it was copied from, such as
	// gorbital.dev/gorbital/authhttp.
	Package string
}

// moduleLayers are the directories an ejected module has below it: the
// library package's internal/<layer> (ADR-0083).
var moduleLayers = []string{"domain", "usecase", "repository", "delivery"}

// Uneject turns the golden app in dir, whose module path is module, into
// the app the templates hold, which orb new then ejects into again: it
// removes each ejected module's directory and the copies of its migrations
// in db/migrations, and points the app's imports of the copies back at the
// library packages. api/surface.json and go.mod are left as they are: the
// caller records the surface again and go mod tidy tidies go.mod.
//
// Templates keep sign-in and organisations in the library, so orb new
// copies them from the version the new app requires, and orb new
// --no-eject writes the app without them.
func Uneject(dir, module string, ejected []Ejected) error {
	migrations := map[string]bool{}
	for _, e := range ejected {
		root := filepath.Join(dir, filepath.FromSlash(e.Dir))
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			return fmt.Errorf("generate: %s has no %s, the copy of %s that orb new writes", dir, e.Dir, e.Package)
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".sql") {
				return err
			}
			sql, err := os.ReadFile(p) //nolint:gosec // a golden app's file
			migrations[string(sql)] = true
			return err
		})
		if err != nil {
			return err
		}
		if err := os.RemoveAll(root); err != nil {
			return err
		}
	}

	// orb eject copies a module's migrations under their library versions,
	// byte for byte, so the copies are the files with the same content.
	dbDir := filepath.Join(dir, "db", "migrations")
	entries, err := os.ReadDir(dbDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		p := filepath.Join(dbDir, e.Name())
		sql, err := os.ReadFile(p) //nolint:gosec // a golden app's file
		if err != nil {
			return err
		}
		if migrations[string(sql)] {
			if err := os.Remove(p); err != nil {
				return err
			}
		}
	}

	back := func(imp string) (string, string, bool) {
		for _, e := range ejected {
			copyPath := module + "/" + e.Dir
			switch {
			case imp == copyPath:
				return e.Package, path.Base(e.Package), true
			case strings.HasPrefix(imp, copyPath+"/"):
				rest := strings.TrimPrefix(imp, copyPath+"/")
				first, _, _ := strings.Cut(rest, "/")
				if slices.Contains(moduleLayers, first) {
					return e.Package + "/internal/" + rest, "", true
				}
				return e.Package + "/" + rest, "", true
			}
		}
		return "", "", false
	}
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && (SkippedDirs[d.Name()] || d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		src, err := os.ReadFile(p) //nolint:gosec // a golden app's file
		if err != nil {
			return err
		}
		out, changed, err := imports.Rewrite(p, src, back)
		if err != nil || !changed {
			return err
		}
		return os.WriteFile(p, out, 0o644) //nolint:gosec // a golden app's file, written as orb new writes it
	})
}
