package main

import (
	"bytes"
	"cmp"
	"embed"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The Methods pages (docs/methods) document every public package of the
// library from its source: the root module gorbital.dev and every module
// under modules/, the same packages apicheck lists in api/*.txt. They are
// read with go/parser and go/doc only, so no build, database or network is
// needed.

// methodsDir is where the Methods pages go, relative to the repository root.
const methodsDir = "docs/methods"

// methodsOverlayDir holds curated text inserted after a package's doc, one
// <slug>.md per package, relative to the repository root.
const methodsOverlayDir = "internal/tools/refdocs/overlay/methods"

// nextVersion is the Since of identifiers not in any released listing.
const nextVersion = "v0.4.0 (unreleased)"

// errMethods reports stale Methods pages or undocumented identifiers.
var errMethods = errors.New("docs/methods is out of date or the library has undocumented API; fix the problems above, then run: go run -C internal/tools/refdocs . -methods -write")

// notLibrary are the top-level directories of the repository with no library
// packages (as in internal/archtest).
var notLibrary = []string{"cli", "docs", "examples", "scripts", "spikes"}

// libPackage is one parsed library package.
type libPackage struct {
	importPath string
	slug       string // page name in docs/methods, without .md
	fset       *token.FileSet
	doc        *doc.Package
	pkgPos     token.Pos // the package clause carrying the package doc, or of the first file
}

// core reports whether the package is in the root module.
func (p *libPackage) core() bool { return !strings.HasPrefix(p.importPath, "gorbital.dev/modules/") }

// title is the package's name on its page: its import path without the
// gorbital.dev/ prefix.
func (p *libPackage) title() string { return strings.TrimPrefix(p.importPath, "gorbital.dev/") }

// slugFor names the page of a package: httpx for gorbital.dev/httpx,
// modules-auth-passkey for gorbital.dev/modules/auth/passkey (as apicheck
// names its listings).
func slugFor(importPath string) string {
	return strings.ReplaceAll(strings.TrimPrefix(importPath, "gorbital.dev/"), "/", "-")
}

// runMethods generates the Methods pages, or checks them and the doc
// comments when write is false.
func runMethods(root string, write bool, out io.Writer) error {
	since, err := loadSince(sinceFS)
	if err != nil {
		return err
	}
	return runMethodsWith(root, since, write, out)
}

// runMethodsWith is runMethods with the releases' identifiers given.
func runMethodsWith(root string, since sinceIndex, write bool, out io.Writer) error {
	pkgs, err := loadLibrary(root)
	if err != nil {
		return err
	}
	overlays, err := readOverlays(filepath.Join(root, filepath.FromSlash(methodsOverlayDir)))
	if err != nil {
		return err
	}
	site := &methodsSite{pkgs: pkgs, since: since, overlays: overlays}
	pages := site.pages()

	dir := filepath.Join(root, filepath.FromSlash(methodsDir))
	var problems []string
	for _, p := range site.problems() {
		problems = append(problems, p.String())
	}
	existing, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, p := range pages {
		wanted[p.file] = true
	}
	if write {
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // committed documentation
			return err
		}
		for _, p := range pages {
			if err := os.WriteFile(filepath.Join(dir, p.file), p.content, 0o644); err != nil { //nolint:gosec // committed documentation
				return err
			}
		}
		for _, f := range existing {
			if !wanted[filepath.Base(f)] {
				if err := os.Remove(f); err != nil {
					return err
				}
			}
		}
		fmt.Fprintf(out, "refdocs: wrote %d pages in %s\n", len(pages), methodsDir)
	} else {
		for _, p := range pages {
			current, err := os.ReadFile(filepath.Join(dir, p.file)) //nolint:gosec // a generated page under the repository
			if err != nil || !bytes.Equal(current, p.content) {
				problems = append(problems, fmt.Sprintf("%s/%s: stale", methodsDir, p.file))
			}
		}
		for _, f := range existing {
			if !wanted[filepath.Base(f)] {
				problems = append(problems, fmt.Sprintf("%s/%s: no such package; remove it", methodsDir, filepath.Base(f)))
			}
		}
	}
	unlisted, err := unlistedPages(root, pages)
	if err != nil {
		return err
	}
	problems = append(problems, unlisted...)
	for _, p := range problems {
		fmt.Fprintln(out, p)
	}
	if len(problems) > 0 {
		return errMethods
	}
	if !write {
		fmt.Fprintf(out, "refdocs: %d Methods pages match the library\n", len(pages))
	}
	return nil
}

// unlistedPages reports pages the Methods tab of docs/docs.json doesn't list,
// so a new package gets a page in the navigation. Without docs/docs.json
// there is nothing to check.
func unlistedPages(root string, pages []page) ([]string, error) {
	nav, err := os.ReadFile(filepath.Join(root, "docs", "docs.json")) //nolint:gosec // a fixed file under the repository
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range pages {
		source := methodsDir + "/" + p.file
		if !bytes.Contains(nav, []byte(`"source": "`+source+`"`)) {
			out = append(out, fmt.Sprintf("docs/docs.json: %s isn't in the Methods tab; add a page with \"source\": %q", source, source))
		}
	}
	return out, nil
}

// loadLibrary parses every public library package under root, sorted by
// import path.
func loadLibrary(root string) ([]*libPackage, error) {
	dirs, err := libraryPackageDirs(root)
	if err != nil {
		return nil, err
	}
	var pkgs []*libPackage
	for _, dir := range dirs {
		p, err := loadPackage(root, dir)
		if err != nil {
			return nil, err
		}
		if p != nil {
			pkgs = append(pkgs, p)
		}
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no library packages under %s", root)
	}
	slices.SortFunc(pkgs, func(a, b *libPackage) int { return cmp.Compare(a.importPath, b.importPath) })
	return pkgs, nil
}

// libraryPackageDirs returns the directories, relative to root and
// slash-separated, holding non-test Go files of the root module and of the
// modules under modules/: everything except the top-level notLibrary
// directories and internal, testdata, hidden and underscore directories.
func libraryPackageDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
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
		if strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") {
			dirs = append(dirs, path.Dir(rel))
		}
		return nil
	})
	slices.Sort(dirs) // a directory's files and subdirectories interleave in the walk
	return slices.Compact(dirs), err
}

// buildContext selects files as apicheck does: gorbital's API is listed as
// Linux builds it.
var buildContext = func() build.Context {
	c := build.Default
	c.GOOS, c.GOARCH, c.CgoEnabled = "linux", "amd64", false
	return c
}()

// loadPackage parses the package in dir (relative to root) with its test
// files, for examples. It returns nil for a command.
func loadPackage(root, dir string) (*libPackage, error) {
	importPath, err := importPathOf(root, dir)
	if err != nil {
		return nil, err
	}
	abs := filepath.Join(root, filepath.FromSlash(dir))
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var name string
	var files, tests []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if ok, err := buildContext.MatchFile(abs, e.Name()); err != nil || !ok {
			if err != nil {
				return nil, err
			}
			continue
		}
		src, err := os.ReadFile(filepath.Join(abs, e.Name())) //nolint:gosec // source files of the repository
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, path.Join(dir, e.Name()), src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			tests = append(tests, f)
			continue
		}
		if name != "" && f.Name.Name != name {
			return nil, fmt.Errorf("%s: packages %s and %s in one directory", dir, name, f.Name.Name)
		}
		name = f.Name.Name
		files = append(files, f)
	}
	if name == "" || name == "main" {
		return nil, nil
	}
	pkgPos := files[0].Package
	for _, f := range files {
		if f.Doc != nil {
			pkgPos = f.Package
			break
		}
	}
	for _, f := range tests {
		if f.Name.Name == name || f.Name.Name == name+"_test" {
			files = append(files, f)
		}
	}
	d, err := doc.NewFromFiles(fset, files, importPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	return &libPackage{importPath: importPath, slug: slugFor(importPath), fset: fset, doc: d, pkgPos: pkgPos}, nil
}

// importPathOf joins the path of the nearest go.mod at or above dir with
// the rest of dir.
func importPathOf(root, dir string) (string, error) {
	for mod := dir; ; mod = path.Dir(mod) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(mod), "go.mod")) //nolint:gosec // go.mod files of the repository
		if err == nil {
			modPath := modulePath(data)
			if modPath == "" {
				return "", fmt.Errorf("%s/go.mod: no module line", mod)
			}
			rest := strings.TrimPrefix(strings.TrimPrefix(dir, mod), "/")
			if mod == "." {
				rest = dir
			}
			if rest == "" || rest == "." {
				return modPath, nil
			}
			return modPath + "/" + rest, nil
		}
		if mod == "." {
			return "", fmt.Errorf("%s: no go.mod at or above it", dir)
		}
	}
}

func modulePath(gomod []byte) string {
	for line := range strings.Lines(string(gomod)) {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// readOverlays reads the curated <slug>.md files; a missing directory has
// none.
func readOverlays(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	overlays := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) //nolint:gosec // committed documentation
		if err != nil {
			return nil, err
		}
		overlays[strings.TrimSuffix(e.Name(), ".md")] = data
	}
	return overlays, nil
}

// ---- Since ----

// sinceFS holds the API listings of each release, copied from api/*.txt at
// its tag (git show v0.1.0:api/<file>), so the check needs no git history.
// One directory per release whose API a reader can depend on: v0.1.0,
// v0.2.0 and v0.3.2. v0.3.0 and v0.3.1 have no directory of their own —
// v0.3.0 is retracted and v0.3.1 requires it, so the v0.3 line's API is
// dated by the release that ships it, v0.3.2. Add the next one here in the
// release commit, and move nextVersion on.
//
//go:embed since
var sinceFS embed.FS

// sinceIndex maps "importpath\tName" (Name, Type or Type.Method) to the
// first release listing it.
type sinceIndex map[string]string

// loadSince reads since/<version>/*.txt, oldest version first, so each
// identifier keeps the release it arrived in.
func loadSince(fsys fs.FS) (sinceIndex, error) {
	versions, err := fs.ReadDir(fsys, "since")
	if err != nil {
		return nil, err
	}
	slices.SortFunc(versions, func(a, b fs.DirEntry) int { return compareVersions(a.Name(), b.Name()) })
	idx := sinceIndex{}
	for _, v := range versions {
		if !v.IsDir() {
			continue
		}
		listings, err := fs.Glob(fsys, "since/"+v.Name()+"/*.txt")
		if err != nil {
			return nil, err
		}
		if len(listings) == 0 {
			return nil, fmt.Errorf("since/%s: no listings", v.Name())
		}
		for _, l := range listings {
			data, err := fs.ReadFile(fsys, l)
			if err != nil {
				return nil, err
			}
			for line := range strings.Lines(string(data)) {
				pkg, ident, ok := parseListingLine(strings.TrimSpace(line))
				if !ok {
					continue
				}
				if _, seen := idx[pkg+"\t"+ident]; !seen {
					idx[pkg+"\t"+ident] = v.Name()
				}
			}
		}
	}
	return idx, nil
}

// of returns the release an identifier arrived in, or nextVersion.
func (s sinceIndex) of(importPath, ident string) string {
	if v, ok := s[importPath+"\t"+ident]; ok {
		return v
	}
	return nextVersion
}

// parseListingLine reads one apicheck line into its package and the
// documented identifier it belongs to: Name for a constant, variable,
// function or type (struct fields and interface methods count as their
// type), Type.Method for a method.
//
//	pkg gorbital.dev/ratelimit, func Per(int, time.Duration) Limit     → Per
//	pkg gorbital.dev/ratelimit, method (*Limiter) Take(…) (…)          → Limiter.Take
//	pkg gorbital.dev/ratelimit, type Decision struct, Allowed bool     → Decision
func parseListingLine(line string) (pkg, ident string, ok bool) {
	rest, ok := strings.CutPrefix(line, "pkg ")
	if !ok {
		return "", "", false
	}
	pkg, rest, ok = strings.Cut(rest, ", ")
	if !ok {
		return "", "", false
	}
	kind, rest, ok := strings.Cut(rest, " ")
	if !ok {
		return "", "", false
	}
	name := func(s string) string {
		if i := strings.IndexAny(s, " ([,"); i >= 0 {
			return s[:i]
		}
		return s
	}
	switch kind {
	case "const", "var", "func", "type":
		ident = name(rest)
	case "method":
		recv, m, found := strings.Cut(rest, ") ")
		if !found {
			return "", "", false
		}
		recv = strings.TrimLeft(strings.TrimPrefix(recv, "("), "*")
		ident = name(recv) + "." + name(m)
	default:
		return "", "", false
	}
	return pkg, ident, ident != "" && !strings.HasPrefix(ident, ".") && !strings.HasSuffix(ident, ".")
}

// compareVersions orders vX.Y.Z names numerically.
func compareVersions(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := range min(len(pa), len(pb)) {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		if errA != nil || errB != nil {
			if c := strings.Compare(pa[i], pb[i]); c != 0 {
				return c
			}
			continue
		}
		if c := cmp.Compare(na, nb); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(pa), len(pb))
}

// ---- Checks ----

// problem is one check failure at a source position.
type problem struct {
	pos   token.Position
	ident string
	msg   string
}

func (p problem) String() string {
	return fmt.Sprintf("%s:%d: %s %s", p.pos.Filename, p.pos.Line, p.ident, p.msg)
}

const (
	msgNoDoc     = "has no doc comment"
	msgNoExample = "is new in " + nextVersion + " and has no Example function"
)

// problems lists exported identifiers without a doc comment and new
// functions, types and methods without an Example, sorted by position.
func (s *methodsSite) problems() []problem {
	var out []problem
	for slug := range s.overlays {
		if !slices.ContainsFunc(s.pkgs, func(p *libPackage) bool { return p.slug == slug }) {
			out = append(out, problem{pos: token.Position{Filename: methodsOverlayDir + "/" + slug + ".md", Line: 1}, ident: slug, msg: "is not a library package (overlay names are page slugs, such as httpx or modules-auth)"})
		}
	}
	for _, p := range s.pkgs {
		out = append(out, s.packageProblems(p)...)
	}
	slices.SortStableFunc(out, func(a, b problem) int {
		return cmp.Or(cmp.Compare(a.pos.Filename, b.pos.Filename), cmp.Compare(a.pos.Line, b.pos.Line), cmp.Compare(a.ident, b.ident))
	})
	return out
}

func (s *methodsSite) packageProblems(p *libPackage) []problem {
	var out []problem
	add := func(pos token.Pos, ident, msg string) {
		out = append(out, problem{pos: p.fset.Position(pos), ident: p.doc.Name + "." + ident, msg: msg})
	}
	if strings.TrimSpace(p.doc.Doc) == "" {
		out = append(out, problem{pos: p.fset.Position(p.pkgPos), ident: "package " + p.doc.Name, msg: msgNoDoc})
	}
	values := func(vs []*doc.Value) {
		for _, v := range vs {
			if strings.TrimSpace(v.Doc) != "" {
				continue
			}
			for _, spec := range v.Decl.Specs {
				vs := spec.(*ast.ValueSpec)
				if vs.Doc != nil || vs.Comment != nil {
					continue
				}
				for _, n := range vs.Names {
					if n.IsExported() {
						add(n.Pos(), n.Name, msgNoDoc)
					}
				}
			}
		}
	}
	funcs := func(fs []*doc.Func) {
		for _, f := range fs {
			if f.Level > 0 {
				continue // promoted from an embedded type: checked where it's declared
			}
			ident := anchorOf(f)
			if strings.TrimSpace(f.Doc) == "" {
				add(f.Decl.Name.Pos(), ident, msgNoDoc)
			}
			if len(f.Examples) == 0 && !implementsStandard(f) && s.since.of(p.importPath, ident) == nextVersion {
				add(f.Decl.Name.Pos(), ident, msgNoExample)
			}
		}
	}
	values(p.doc.Consts)
	values(p.doc.Vars)
	funcs(p.doc.Funcs)
	for _, t := range p.doc.Types {
		pos := t.Decl.Specs[0].(*ast.TypeSpec).Name.Pos()
		if strings.TrimSpace(t.Doc) == "" {
			add(pos, t.Name, msgNoDoc)
		}
		if len(t.Examples) == 0 && s.since.of(p.importPath, t.Name) == nextVersion {
			add(pos, t.Name, msgNoExample)
		}
		values(t.Consts)
		values(t.Vars)
		funcs(t.Funcs)
		funcs(t.Methods)
	}
	return out
}

// standardMethods are methods whose meaning a standard interface defines
// (error, fmt.Stringer, io.Writer, http.ResponseWriter's writes, http.Handler, and the
// Unwrap convention of errors and http.ResponseController). An Example would
// only repeat the interface, so they are exempt from the example rule; they
// still need doc comments.
var standardMethods = map[string]bool{
	"Error": true, "String": true, "Write": true, "WriteHeader": true,
	"ServeHTTP": true, "Unwrap": true,
}

// implementsStandard reports whether f is a method named after a standard
// interface's method.
func implementsStandard(f *doc.Func) bool {
	return f.Recv != "" && standardMethods[f.Name]
}

// anchorOf is a function's identifier and anchor: Name, or Type.Method.
func anchorOf(f *doc.Func) string {
	if f.Recv == "" {
		return f.Name
	}
	return recvName(f.Recv) + "." + f.Name
}

// recvName drops the pointer and type parameters of a receiver: *Cache[K] is
// Cache.
func recvName(recv string) string {
	recv = strings.TrimPrefix(recv, "*")
	if i := strings.IndexByte(recv, '['); i >= 0 {
		recv = recv[:i]
	}
	return recv
}
