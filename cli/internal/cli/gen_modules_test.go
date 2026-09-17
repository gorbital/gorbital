package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// newModulesApp creates an app using gorbital.Main with modules of every
// kind orb gen modules must tell apart, and makes it the working directory.
func newModulesApp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/shelfie\n\ngo 1.26.0\n\nrequire gorbital.dev/gorbital v0.2.0\n")
	files := map[string]string{
		// A module in the layered layout: module.go at the top.
		"books/module.go":      "package books\n\nimport \"gorbital.dev/gorbital\"\n\nfunc Module() gorbital.Module { return gorbital.Module{Name: \"books\"} }\n",
		"books/domain/book.go": "package domain\n\ntype Book struct{}\n",
		"books/module_test.go": "package books\n",
		// The import renamed.
		"reviews/reviews.go": "package reviews\n\nimport g \"gorbital.dev/gorbital\"\n\n// Module is the reviews module.\nfunc Module() g.Module { return g.Module{Name: \"reviews\"} }\n",
		// A package whose name clashes with the generated file's imports.
		"gorbital_admin/module.go": "package gorbital\n\nimport gb \"gorbital.dev/gorbital\"\n\nfunc Module() gb.Module { return gb.Module{Name: \"admin\"} }\n",
		// Not modules.
		"helpers/helpers.go":     "package helpers\n\nfunc Module() int { return 1 }\n",
		"legacy/module.go":       "package legacy\n\ntype Module struct{}\n\nfunc (Module) Module() Module { return Module{} }\n",
		"testonly/x_test.go":     "package testonly\n\nimport \"gorbital.dev/gorbital\"\n\nfunc Module() gorbital.Module { return gorbital.Module{} }\n",
		"withargs/module.go":     "package withargs\n\nimport \"gorbital.dev/gorbital\"\n\nfunc Module(name string) gorbital.Module { return gorbital.Module{Name: name} }\n",
		"_draft/module.go":       "package draft\n\nimport \"gorbital.dev/gorbital\"\n\nfunc Module() gorbital.Module { return gorbital.Module{} }\n",
		"otherlib/module.go":     "package otherlib\n\nimport gorbital \"example.com/other\"\n\nfunc Module() gorbital.Module { return gorbital.Module{} }\n",
		"testdata/app/module.go": "package app\n\nimport \"gorbital.dev/gorbital\"\n\nfunc Module() gorbital.Module { return gorbital.Module{} }\n",
	}
	for path, content := range files {
		writeFile(t, filepath.Join(dir, "internal", "modules", filepath.FromSlash(path)), content)
	}
	t.Chdir(dir)
	return dir
}

func TestGenModules(t *testing.T) {
	dir := newModulesApp(t)

	code, out, errOut := runOrb(t, "gen", "modules", "--dry-run")
	if code != 0 || !strings.Contains(out, "Would write (dry run) internal/modules/modules.gen.go: 3 modules: books, gorbital_admin, reviews") {
		t.Fatalf("orb gen modules --dry-run = %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, modulesGenPath)); err == nil {
		t.Fatal("--dry-run wrote the file")
	}

	code, out, errOut = runOrb(t, "gen", "modules")
	if code != 0 || !strings.Contains(out, "✓ Wrote internal/modules/modules.gen.go: 3 modules") {
		t.Fatalf("orb gen modules = %d %q %q", code, out, errOut)
	}
	got := readFile(t, filepath.Join(dir, modulesGenPath))
	golden := filepath.Join(repoRoot(t), "cli", "internal", "cli", "testdata", "gen-modules", "modules.gen.go.golden")
	if *updateJSON {
		writeFile(t, golden, got)
	}
	if want := readFile(t, golden); got != want {
		t.Errorf("modules.gen.go:\n%s\nwant (%s, rewrite with -update):\n%s", got, golden, want)
	}

	// Run from the package directory, as go generate does, and again: nothing to do.
	t.Chdir(filepath.Join(dir, "internal", "modules"))
	code, out, _ = runOrb(t, "gen", "modules", "--json")
	var res genModulesResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Changed || !slices.Equal(res.Modules, []string{"books", "gorbital_admin", "reviews"}) {
		t.Errorf("orb gen modules --json on an up-to-date file = %d %s", code, out)
	}

	// A removed module is removed from the list.
	if err := os.RemoveAll(filepath.Join(dir, "internal", "modules", "reviews")); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runOrb(t, "gen", "modules")
	if code != 0 || !strings.Contains(out, "2 modules: books, gorbital_admin") || strings.Contains(readFile(t, filepath.Join(dir, modulesGenPath)), "reviews") {
		t.Errorf("after removing reviews: %d %q\n%s", code, out, readFile(t, filepath.Join(dir, modulesGenPath)))
	}
}

func TestGenModulesErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/empty\n")
	t.Chdir(dir)
	if code, _, errOut := runOrb(t, "gen", "modules"); code != 1 || !strings.Contains(errOut, "no internal/modules directory") {
		t.Errorf("without internal/modules = %d %q", code, errOut)
	}
	if code, _, errOut := runOrb(t, "gen", "modules", "extra"); code != 2 || !strings.Contains(errOut, "takes no arguments") {
		t.Errorf("with an argument = %d %q", code, errOut)
	}
	writeFile(t, filepath.Join(dir, "internal", "modules", "broken", "module.go"), "package broken\n\nfunc Module( {\n")
	if code, _, errOut := runOrb(t, "gen", "modules"); code != 1 || !strings.Contains(errOut, "parse") {
		t.Errorf("with a file that doesn't parse = %d %q", code, errOut)
	}
}

func TestDevGeneratesModules(t *testing.T) {
	dir := newModulesApp(t)
	var out bytes.Buffer
	d := newDevRunner(&out)

	// No modules.gen.go: the app hasn't opted in, as v0.1 apps haven't.
	d.generateModules()
	if _, err := os.Stat(filepath.Join(dir, modulesGenPath)); err == nil || out.Len() != 0 {
		t.Fatalf("orb dev created modules.gen.go in an app without one: %q", out.String())
	}

	writeFile(t, filepath.Join(dir, modulesGenPath), "// Code generated by orb gen modules. DO NOT EDIT.\n\npackage modules\n")
	d.generateModules()
	if !strings.Contains(out.String(), "orb: updated internal/modules/modules.gen.go (3 modules: books, gorbital_admin, reviews)") ||
		!strings.Contains(readFile(t, filepath.Join(dir, modulesGenPath)), "reviews.Module(),") {
		t.Errorf("orb dev with a stale modules.gen.go: %q", out.String())
	}
	out.Reset()
	d.generateModules()
	if out.Len() != 0 {
		t.Errorf("orb dev with an up-to-date modules.gen.go printed %q", out.String())
	}
}

func TestMigrateCommands(t *testing.T) {
	v01 := t.TempDir()
	writeFile(t, filepath.Join(v01, "go.mod"), "module example.com/acme\n\nrequire gorbital.dev v0.1.0\n")
	writeFile(t, filepath.Join(v01, "cmd", "migrate", "main.go"), "package main\n")
	app := t.TempDir()
	writeFile(t, filepath.Join(app, "go.mod"), "module example.com/shelfie\n\nrequire gorbital.dev/gorbital v0.2.0\n")

	for _, tt := range []struct {
		dir  string
		args []string
		want string
	}{
		{v01, nil, "run ./cmd/migrate"},
		{v01, []string{"--redo"}, "run ./cmd/migrate --redo"},
		{app, nil, "run ./cmd/api migrate"},
		{app, []string{"--status", "--json"}, "run ./cmd/api migrate --status --json"},
		{app, []string{"--down"}, "run ./cmd/api migrate-down"},
		{app, []string{"--redo"}, "run ./cmd/api migrate-down; run ./cmd/api migrate"},
	} {
		var got []string
		for _, c := range migrateCommands(tt.dir, tt.args) {
			got = append(got, strings.Join(c, " "))
		}
		if strings.Join(got, "; ") != tt.want {
			t.Errorf("migrateCommands(%s, %q) = %q, want %q", filepath.Base(tt.dir), tt.args, got, tt.want)
		}
	}
}
