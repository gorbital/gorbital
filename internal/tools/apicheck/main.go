// Command apicheck records and checks the exported Go API of gorbital's
// library modules (ADR-0054).
//
// For the root module and every module under modules/, it lists each
// exported constant, variable, function, type, struct field, interface
// method and method of the public packages (internal packages and commands
// excluded) as sorted lines in the style of Go's own api/go1.txt:
//
//	pkg gorbital.dev/ratelimit, func Per(int, time.Duration) Limit
//	pkg gorbital.dev/ratelimit, method (*Limiter) Take(context.Context, string) (Decision, error)
//	pkg gorbital.dev/ratelimit, type Decision struct, Allowed bool
//
// The listings are committed under api/ at the repository root. Without
// flags, apicheck compares the current API with them and fails when a line
// is missing (a removed or changed identifier: a breaking change) or new (an
// addition to record). Record additions with -write:
//
//	go run -C internal/tools/apicheck . -write
package main

import (
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
	write := flag.Bool("write", false, "rewrite the listings in api/ from the current API")
	flag.Parse()
	if err := run(*root, *write, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "apicheck:", err)
		os.Exit(1)
	}
}

// errIncompatible reports listings that don't match the API.
var errIncompatible = errors.New("the API doesn't match the listings in api/")

func run(root string, write bool, out io.Writer) error {
	if root == "" {
		var err error
		if root, err = findRoot(); err != nil {
			return err
		}
	}
	modules, err := libraryModules(root)
	if err != nil {
		return err
	}
	failed := false
	for _, dir := range modules {
		lines, modulePath, err := listModule(filepath.Join(root, dir))
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		path := filepath.Join(root, "api", listingName(modulePath))
		if write {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // a directory in the repository, like the 0o644 listing it holds
				return err
			}
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil { //nolint:gosec // committed to the repository
				return err
			}
			fmt.Fprintf(out, "wrote api/%s (%d lines)\n", listingName(modulePath), len(lines))
			continue
		}
		recorded, err := readListing(path)
		if err != nil {
			return fmt.Errorf("%w; record it with: go run -C internal/tools/apicheck . -write", err)
		}
		removed, added := diff(recorded, lines)
		for _, l := range removed {
			failed = true
			fmt.Fprintf(out, "api/%s: removed or changed (breaking): %s\n", listingName(modulePath), l)
		}
		for _, l := range added {
			failed = true
			fmt.Fprintf(out, "api/%s: new, not recorded: %s\n", listingName(modulePath), l)
		}
	}
	if failed {
		fmt.Fprintln(out, "\nA removed or changed line breaks callers: restore the API, or keep the old identifier as a deprecated wrapper (ADR-0015).\nRecord additions with: go run -C internal/tools/apicheck . -write")
		return errIncompatible
	}
	if !write {
		fmt.Fprintf(out, "the API of %d modules matches api/\n", len(modules))
	}
	return nil
}

// findRoot walks up from the working directory to the directory whose go.mod
// declares module gorbital.dev.
func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod")) //nolint:gosec // the repository's own files
		if err == nil && modulePathOf(data) == "gorbital.dev" {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no gorbital.dev module above the working directory; pass -root")
		}
		dir = parent
	}
}

// libraryModules returns the library modules by directory relative to root:
// ".", every module under modules/, and the composition module in gorbital/.
func libraryModules(root string) ([]string, error) {
	modules := []string{"."}
	err := filepath.WalkDir(filepath.Join(root, "modules"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")) {
			return filepath.SkipDir
		}
		if d.Name() == "go.mod" {
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			modules = append(modules, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(modules[1:])
	if err != nil {
		return nil, err
	}
	// The composition module (ADR-0081) sits beside modules/.
	if _, err := os.Stat(filepath.Join(root, "gorbital", "go.mod")); err == nil {
		modules = append(modules, "gorbital")
	}
	return modules, nil
}

func modulePathOf(gomod []byte) string {
	for line := range strings.Lines(string(gomod)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// listingName is the file in api/ for a module: gorbital.dev.txt for the
// root module, modules-mail-resend.txt for gorbital.dev/modules/mail/resend.
func listingName(modulePath string) string {
	if modulePath == "gorbital.dev" {
		return "gorbital.dev.txt"
	}
	return strings.ReplaceAll(strings.TrimPrefix(modulePath, "gorbital.dev/"), "/", "-") + ".txt"
}

func readListing(path string) ([]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the repository's own files
	if err != nil {
		return nil, err
	}
	var lines []string
	for line := range strings.Lines(string(data)) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// diff returns the recorded lines missing from current, and the current lines
// not recorded.
func diff(recorded, current []string) (removed, added []string) {
	for _, l := range recorded {
		if !slices.Contains(current, l) {
			removed = append(removed, l)
		}
	}
	for _, l := range current {
		if !slices.Contains(recorded, l) {
			added = append(added, l)
		}
	}
	return removed, added
}
