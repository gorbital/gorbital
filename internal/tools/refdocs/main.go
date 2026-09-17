// Command refdocs generates the reference pages in docs/reference from the
// golden apps' real declarations: error codes, audit actions, permissions
// and roles, runtime settings and jobs (the names recorded in each app's
// api/surface.json, ADR-0054, with what they mean).
//
// Golden app code lives in internal packages that nothing outside the app may
// import, and the apps must not carry repository-specific code (they are the
// templates of generated apps). So refdocs adds a test file to
// examples/<app>/cmd/api for one run with go test -overlay, without writing
// into the app: the test builds the app with main.go's options on a migrated
// test database, reads the permission catalogs, the settings store, the job
// definitions and the source of the app and of the gorbital packages it
// links, and writes JSON that refdocs renders. It runs on
// examples/full-multi, the superset, and on examples/full-single to mark what
// only multi-tenant apps have.
//
// Descriptions the code doesn't carry (when an audit action is recorded,
// what some error codes mean) are in descriptions.json next to this file.
//
// It needs the test database (docker compose up -d --wait and
// GORBITAL_TEST_DATABASE_URL). Without flags it checks the pages; -write
// rewrites them:
//
//	go run -C internal/tools/refdocs .          # check (CI)
//	go run -C internal/tools/refdocs . -write   # after adding codes, actions, permissions, settings or jobs
//
// With -methods it generates the Methods pages instead: docs/methods/<slug>.md
// for every public package of the library (the root module and every module
// under modules/, the packages apicheck lists) and docs/methods/index.md,
// from doc comments and Example functions, read with go/parser and go/doc.
// Each identifier shows the release it arrived in, from the API listings of
// each release frozen in since/<version>/. Curated text for a package goes in
// overlay/methods/<slug>.md. It needs no database. Without -write it checks
// the pages and fails on an exported identifier without a doc comment, on
// a function, type or method added since the last release without an
// Example function, and on a page docs/docs.json doesn't list:
//
//	go run -C internal/tools/refdocs . -methods          # check (CI)
//	go run -C internal/tools/refdocs . -methods -write   # after changing exported API or its doc comments
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	root := flag.String("root", "", "repository root (default: found from the working directory)")
	write := flag.Bool("write", false, "rewrite the pages in docs/reference (docs/methods with -methods)")
	methods := flag.Bool("methods", false, "generate or check the Methods pages in docs/methods from the library source")
	flag.Parse()
	var err error
	if *methods {
		err = runMethodsIn(*root, *write, os.Stdout)
	} else {
		err = run(*root, *write, os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "refdocs:", err)
		os.Exit(1)
	}
}

// errStale reports pages that don't match the golden apps.
var errStale = errors.New("docs/reference is out of date; run: go run -C internal/tools/refdocs . -write")

// referenceDir is where the pages go, relative to the repository root.
const referenceDir = "docs/reference"

func run(root string, write bool, out io.Writer) error {
	if root == "" {
		var err error
		if root, err = findRoot(); err != nil {
			return err
		}
	}
	if os.Getenv("GORBITAL_TEST_DATABASE_URL") == "" {
		return errors.New("the golden apps need the test database: run docker compose up -d --wait and export GORBITAL_TEST_DATABASE_URL (see CONTRIBUTING.md)")
	}
	desc, err := loadDescriptions(filepath.Join(root, "internal", "tools", "refdocs", "descriptions.json"))
	if err != nil {
		return err
	}
	multi, err := dumpApp(root, "full-multi")
	if err != nil {
		return err
	}
	single, err := dumpApp(root, "full-single")
	if err != nil {
		return err
	}

	ref := newReference(multi, single, desc)
	for _, w := range ref.warnings() {
		fmt.Fprintln(out, "warning:", w)
	}

	stale := false
	for _, p := range ref.pages() {
		path := filepath.Join(root, referenceDir, p.file)
		if write {
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return err
			}
			if err := os.WriteFile(path, p.content, 0o644); err != nil { //nolint:gosec // committed documentation
				return err
			}
			continue
		}
		current, err := os.ReadFile(path) //nolint:gosec // a fixed page under the repository
		if err != nil || !bytes.Equal(current, p.content) {
			fmt.Fprintf(out, "stale: %s/%s\n", referenceDir, p.file)
			stale = true
		}
	}
	if write {
		fmt.Fprintf(out, "refdocs: wrote %d pages in %s\n", len(ref.pages()), referenceDir)
		return nil
	}
	if stale {
		return errStale
	}
	return nil
}

// runMethodsIn runs the Methods mode from root, or the repository found from
// the working directory.
func runMethodsIn(root string, write bool, out io.Writer) error {
	if root == "" {
		var err error
		if root, err = findRoot(); err != nil {
			return err
		}
	}
	return runMethods(root, write, out)
}

// findRoot walks up from the working directory to the gorbital.dev module.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // go.mod files above the working directory
		if err == nil && slices.Contains(strings.Fields(firstLine(data)), "gorbital.dev") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no gorbital.dev module above the working directory; pass -root")
		}
		dir = parent
	}
}

func firstLine(data []byte) string {
	for line := range strings.Lines(string(data)) {
		if strings.HasPrefix(line, "module ") {
			return line
		}
	}
	return ""
}
