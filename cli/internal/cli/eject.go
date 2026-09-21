package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/imports"
	"gorbital.dev/cli/internal/recipes"
)

// An ejectRemovedMessage is an error whose whole value is a page of prose
// for a person: nothing wraps it, Main prints it and exits. It is a type
// of its own rather than errors.New so that it can be several sentences,
// capitalised and punctuated, which is what the reader needs and what the
// rules for an error fragment forbid.
type ejectRemovedMessage string

func (e ejectRemovedMessage) Error() string { return string(e) }

// errEjectRemoved answers `orb eject`, removed in v0.2.2 (ADR-0092). The
// name stays registered for one release so a script or an old page gets an
// answer rather than "unknown command".
const errEjectRemoved = ejectRemovedMessage(`orb eject was removed in v0.2.2.

Sign-in and organisations are already in your app, under internal/modules:
orb new put them there. There is nothing to eject.

The library keeps what you should never write yourself -- password hashing,
session tokens, TOTP, API-key hashing -- and no command moves it into an
app. To change how sign-in behaves, use the options and hooks in
gorbital.dev/gorbital/authhttp (see docs/guides/the-code-in-your-repo.md).`)

// An ejectableModule is a built-in module orb new and orb add copy into
// apps. orb eject is gone; the planner below is what those commands run.
type ejectableModule struct {
	// name is what orb eject takes and the directory under
	// internal/modules, such as auth.
	name string
	// pkg is the package under gorbital.dev/gorbital, such as authhttp.
	pkg string
	// constructor is what cmd/api calls to add the module, such as New.
	constructor string
	// about says what the module is.
	about string
}

func (m ejectableModule) importPath() string { return gorbitalImportPath + "/" + m.pkg }

// dir is the module's directory in the app, slash-separated.
func (m ejectableModule) dir() string { return "internal/modules/" + m.name }

// ejectableModules are the modules orb eject copies, sorted by name.
var ejectableModules = []ejectableModule{
	{name: "auth", pkg: "authhttp", constructor: "New", about: "sign-in"},
	{name: "flags", pkg: "flagshttp", constructor: "Module", about: "client feature flags"},
	{name: "mailevents", pkg: "mailevents", constructor: "Module", about: "email events"},
	{name: "ops", pkg: "opshttp", constructor: "Module", about: "the operations API"},
	{name: "orgs", pkg: "orgshttp", constructor: "Module", about: "organisations"},
}

func lookupEjectable(name string) (ejectableModule, bool) {
	i := slices.IndexFunc(ejectableModules, func(m ejectableModule) bool { return m.name == name })
	if i < 0 {
		return ejectableModule{}, false
	}
	return ejectableModules[i], true
}

// moduleLayers are the directories an app module has below it (ADR-0083).
var moduleLayers = []string{"domain", "usecase", "repository", "delivery"}

// noEjectDirective starts a line, before the package clause, of a library
// test file orb eject doesn't copy; the reason follows it.
const noEjectDirective = "//orb:noeject"

type ejectResult struct {
	Module string `json:"module"`
	// Package is the library package the module was copied from.
	Package string `json:"package"`
	// Version is the gorbital.dev/gorbital version it was copied at.
	Version   string `json:"version"`
	Directory string `json:"directory"`
	// Files are the module's files created in Directory.
	Files []string `json:"files"`
	// Migrations are the migrations copied into db/migrations.
	Migrations []string `json:"migrations"`
	// Modified are the app's files whose imports now name the copy, and
	// gorbital.lock.
	Modified []string `json:"modified"`
	// NotCopied are the library's own tests, left out.
	NotCopied []ejectSkipped `json:"not_copied"`
	// Notes say what orb eject left as it was, such as migrations the app
	// already had.
	Notes  []string `json:"notes"`
	Tidied bool     `json:"tidied"`
	// Surface reports that api/surface.json was recorded again: the module's
	// names are the app's own now.
	Surface bool `json:"surface_recorded"`
	DryRun  bool `json:"dry_run"`
}

// ejectSkipped is a library file orb eject didn't copy, and why.
type ejectSkipped struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// A librarySource is the gorbital.dev/gorbital module an app builds with.
type librarySource struct {
	// Version is the version go.mod requires.
	Version string
	// Dir holds its source: the module cache, or a replace directive's
	// directory.
	Dir string
}

// resolveLibrary finds the gorbital.dev/gorbital module the app in dir
// builds with, as go list reports it, downloading it when the module cache
// doesn't have it.
func resolveLibrary(ctx context.Context, dir string) (librarySource, error) {
	var stderr bytes.Buffer
	out, err := goOutputIn(ctx, dir, &stderr, "list", "-m", "-json", gorbitalImportPath)
	if err != nil {
		return librarySource{}, fmt.Errorf("find %s with go list: %w: %s", gorbitalImportPath, err, strings.TrimSpace(stderr.String()))
	}
	var mod struct {
		Version string
		Dir     string
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return librarySource{}, fmt.Errorf("read go list's output: %w", err)
	}
	if mod.Dir == "" && mod.Version != "" {
		stderr.Reset()
		out, err := goOutputIn(ctx, dir, &stderr, "mod", "download", "-json", gorbitalImportPath+"@"+mod.Version)
		if err != nil {
			return librarySource{}, fmt.Errorf("download %s@%s: %w: %s", gorbitalImportPath, mod.Version, err, strings.TrimSpace(stderr.String()))
		}
		if err := json.Unmarshal(out, &mod); err != nil {
			return librarySource{}, fmt.Errorf("read go mod download's output: %w", err)
		}
	}
	if mod.Dir == "" {
		return librarySource{}, fmt.Errorf("go list reports no source directory for %s", gorbitalImportPath)
	}
	return librarySource{Version: cmpOr(mod.Version, "(local)"), Dir: mod.Dir}, nil
}

func goOutputIn(ctx context.Context, dir string, stderr io.Writer, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir, cmd.Stderr = dir, stderr
	return cmd.Output()
}

// planEjectFrom plans copying module m from lib into the app. lock is the
// app's gorbital.lock (lockExists false when it has none), whose ejected
// modules the copy's imports name. It is planEject without the checks of
// the app, for callers that did them.
func planEjectFrom(app appInfo, m ejectableModule, lib librarySource, lock lockFile, lockExists bool, now time.Time) (genplan.Plan, error) {
	src := filepath.Join(lib.Dir, m.pkg)
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return genplan.Plan{}, fmt.Errorf("%s %s has no %s package; modules are copied into apps from v0.2.0 on", gorbitalImportPath, lib.Version, m.pkg)
	}
	res := ejectResult{
		Module: m.name, Package: m.importPath(), Version: lib.Version, Directory: m.dir(),
		Files: []string{}, Migrations: []string{}, Modified: []string{}, NotCopied: []ejectSkipped{}, Notes: []string{},
	}
	owned := []ejectableModule{m}
	for _, e := range lock.Ejected {
		if other, ok := lookupEjectable(e.Module); ok {
			owned = append(owned, other)
		}
	}
	rewrite := ejectImportRewriter(app.module, owned)

	plan := genplan.Plan{Generator: "eject", Name: m.name}
	copied, hash, err := copyEjectedModule(src, m, rewrite, &res)
	if err != nil {
		return genplan.Plan{}, err
	}
	plan.Changes = append(plan.Changes, copied...)

	migrations, err := ejectMigrations(app.dir, lib.Dir, src, m, &res)
	if err != nil {
		return genplan.Plan{}, err
	}
	plan.Changes = append(plan.Changes, migrations...)

	modified, err := rewriteAppImports(app, m, rewrite)
	if err != nil {
		return genplan.Plan{}, err
	}
	plan.Changes = append(plan.Changes, modified...)
	for _, c := range modified {
		res.Modified = append(res.Modified, c.Path)
	}

	next := lock
	if !lockExists {
		next = lockFile{APIVersion: LockAPIVersion, Orb: lockOrb{Version: Version, Revision: buildRevision()}, Files: []lockedFile{}}
	}
	next.Ejected = append(slices.Clone(lock.Ejected), lockEjected{
		Module: m.name, Package: m.importPath(), Version: lib.Version, Date: now.Format(time.DateOnly), SHA256: hash,
	})
	slices.SortFunc(next.Ejected, func(a, b lockEjected) int { return strings.Compare(a.Module, b.Module) })
	encoded, err := next.encode()
	if err != nil {
		return genplan.Plan{}, err
	}
	if lockExists {
		before, err := os.ReadFile(filepath.Join(app.dir, lockPath))
		if err != nil {
			return genplan.Plan{}, err
		}
		plan.Changes = append(plan.Changes, genplan.Change{Path: lockPath, Kind: genplan.Modify, Before: before, Content: encoded})
	} else {
		plan.Changes = append(plan.Changes, genplan.Change{Path: lockPath, Kind: genplan.Create, Content: encoded})
	}
	res.Modified = append(res.Modified, lockPath)

	plan.Summary = ejectSummary(app, m, res)
	plan.Next = []string{
		"go build ./... && go vet ./...",
		"go test ./...",
		"go run ./cmd/api openapi --dir api (the document doesn't change)",
		fmt.Sprintf("git add -A && git commit -m 'Eject %s'", m.name),
	}
	plan.Result = res
	return plan, nil
}

func ejectSummary(app appInfo, m ejectableModule, res ejectResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  Module:      %s, %d files: the root package, its layers and tests\n", res.Directory, len(res.Files))
	var imports []string
	for _, p := range res.Modified {
		if p != lockPath {
			imports = append(imports, p)
		}
	}
	if len(imports) > 3 {
		imports = append(imports[:3:3], fmt.Sprintf("%d more files", len(imports)-3))
	}
	fmt.Fprintf(&b, "  Imports:     %s now import %s/%s\n", strings.Join(imports, ", "), app.module, res.Directory)
	switch len(res.Migrations) {
	case 0:
		b.WriteString("  Migrations:  none to copy\n")
	default:
		fmt.Fprintf(&b, "  Migrations:  %d copied into db/migrations under the same versions\n", len(res.Migrations))
	}
	if len(res.NotCopied) > 0 {
		fmt.Fprintf(&b, "  Not copied:  %d tests of the library itself (%s)\n", len(res.NotCopied), noEjectDirective)
	}
	fmt.Fprintf(&b, "  Lock:        %s records %s as ejected from %s\n", lockPath, m.name, res.Version)
	for _, n := range res.Notes {
		fmt.Fprintf(&b, "  Note:        %s\n", n)
	}
	return b.String()
}

// ejectImportRewriter returns a function mapping a library import path of
// one of modules, or of their internal packages, to the app's copy, with
// the package name to import it under.
func ejectImportRewriter(appModule string, modules []ejectableModule) func(string) (string, string, bool) {
	return func(imp string) (string, string, bool) {
		for _, m := range modules {
			root := m.importPath()
			target := appModule + "/" + m.dir()
			switch {
			case imp == root:
				return target, m.pkg, true
			case strings.HasPrefix(imp, root+"/internal/"):
				return target + "/" + strings.TrimPrefix(imp, root+"/internal/"), "", true
			case strings.HasPrefix(imp, root+"/"):
				return target + "/" + strings.TrimPrefix(imp, root+"/"), "", true
			}
		}
		return "", "", false
	}
}

// ejectedRewriter maps the library packages of the modules the app's
// gorbital.lock records as ejected to the app's copies, as orb eject maps
// them; nil when it records none.
func ejectedRewriter(app appInfo) func(string) (string, string, bool) {
	lock, err := readLock(app.dir)
	if err != nil || len(lock.Ejected) == 0 {
		return nil
	}
	var owned []ejectableModule
	for _, e := range lock.Ejected {
		if m, ok := lookupEjectable(e.Module); ok {
			owned = append(owned, m)
		}
	}
	return ejectImportRewriter(app.module, owned)
}

// importEjected changes generated Go files that import the library package
// of a module the app has ejected, such as an organisation module's tests
// importing orgshttp, to import the app's copy: the app builds with its own
// sign-in and organisations, so their tests should too.
func importEjected(app appInfo, files []recipes.JobFile) ([]recipes.JobFile, error) {
	rewrite := ejectedRewriter(app)
	if rewrite == nil {
		return files, nil
	}
	out := slices.Clone(files)
	for i, f := range out {
		if !strings.HasSuffix(f.Path, ".go") {
			continue
		}
		content, changed, err := rewriteGoImports(f.Path, f.Content, rewrite)
		if err != nil {
			return nil, err
		}
		if changed {
			out[i].Content = content
		}
	}
	return out, nil
}

// rewriteGoImports changes src's imports that rewrite maps, keeping
// everything else byte for byte, then formats it (imports.Rewrite).
func rewriteGoImports(filename string, src []byte, rewrite func(string) (string, string, bool)) ([]byte, bool, error) {
	return imports.Rewrite(filename, src, rewrite)
}

// noEjectReason returns the reason of a //orb:noeject directive before
// src's package clause.
func noEjectReason(src []byte) (string, bool) {
	for line := range strings.Lines(string(src)) {
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "package ") {
			break
		}
		if rest, ok := strings.CutPrefix(line, noEjectDirective); ok && (rest == "" || rest[0] == ' ') {
			return cmpOr(strings.TrimSpace(rest), "a test of the library itself"), true
		}
	}
	return "", false
}

// copyEjectedModule plans the module's files, from the package at src, in
// the app: internal/<layer>/… becomes <layer>/…, Go imports of owned modules
// name the app's copies, and test files marked //orb:noeject are left out.
// It returns the changes and a hash of the package's source.
func copyEjectedModule(src string, m ejectableModule, rewrite func(string) (string, string, bool), res *ejectResult) ([]genplan.Change, string, error) {
	var changes []genplan.Change
	targets := map[string]string{}
	h := sha256.New()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if parts := strings.Split(rel, "/"); len(parts) == 2 && parts[0] == "internal" && !slices.Contains(moduleLayers, parts[1]) {
				return fmt.Errorf("%s/internal/%s isn't under one of the layers an app module has (%s), so this version can't be ejected", m.importPath(), parts[1], strings.Join(moduleLayers, ", "))
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		content, err := os.ReadFile(p) //nolint:gosec // the library's source, found by go list
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", rel, len(content))
		h.Write(content)

		target := rel
		if rest, ok := strings.CutPrefix(rel, "internal/"); ok {
			if !strings.Contains(rest, "/") {
				return fmt.Errorf("%s has %s directly in internal/, which an app module has no place for", m.importPath(), rel)
			}
			target = rest
		}
		if prev, dup := targets[target]; dup {
			return fmt.Errorf("%s: %s and %s would both be %s/%s", m.importPath(), prev, rel, m.dir(), target)
		}
		targets[target] = rel
		if strings.HasSuffix(rel, ".go") {
			if reason, skip := noEjectReason(content); skip {
				if !strings.HasSuffix(rel, "_test.go") {
					return fmt.Errorf("%s/%s is marked %s, but only tests can be left out", m.importPath(), rel, noEjectDirective)
				}
				res.NotCopied = append(res.NotCopied, ejectSkipped{Path: rel, Reason: reason})
				return nil
			}
			if content, _, err = rewriteGoImports(rel, content, rewrite); err != nil {
				return err
			}
			if imp := libraryInternalImport(content); imp != "" {
				return fmt.Errorf("%s/%s imports %s, which an app can't import, so %s %s can't be ejected", m.importPath(), rel, imp, m.pkg, "at this version")
			}
		}
		out := m.dir() + "/" + target
		changes = append(changes, genplan.Change{Path: out, Kind: genplan.Create, Content: content})
		res.Files = append(res.Files, out)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	slices.Sort(res.Files)
	return changes, hex.EncodeToString(h.Sum(nil)), nil
}

// hashLibraryPackage hashes the package's source as copyEjectedModule does.
func hashLibraryPackage(dir string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		content, err := os.ReadFile(p) //nolint:gosec // the library's source, found by go list
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(content))
		h.Write(content)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil)), err
}

// libraryInternalImport returns an import of src that is internal to the
// library, which an app can't import, or "".
func libraryInternalImport(src []byte) string {
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly)
	if err != nil {
		return ""
	}
	for _, spec := range file.Imports {
		imp, _ := strconv.Unquote(spec.Path.Value)
		if rest, ok := strings.CutPrefix(imp, gorbitalImportPath+"/"); ok && (strings.HasPrefix(rest, "internal/") || strings.Contains(rest, "/internal/")) {
			return imp
		}
	}
	return ""
}

// ejectMigrations plans the module's migrations, as its Module declares
// them with gorbital.Migration literals in the package at src, in the app's
// db/migrations under the same versions and names gorbital.Migrate gives
// them, so the merge treats each pair as one migration.
func ejectMigrations(appDir, libDir, src string, m ejectableModule, res *ejectResult) ([]genplan.Change, error) {
	migrations, err := declaredMigrations(libDir, src, m)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, nil
	}
	dbDir := filepath.Join(appDir, "db", "migrations")
	entries, err := os.ReadDir(dbDir)
	if errors.Is(err, fs.ErrNotExist) {
		res.Notes = append(res.Notes, "the app has no db/migrations, so the module's migrations stay only in its Module.Migrations")
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var changes []genplan.Change
	for _, mig := range migrations {
		name := fmt.Sprintf("%d_%s.sql", mig.version, mig.name)
		prefix := strconv.FormatInt(mig.version, 10) + "_"
		i := slices.IndexFunc(entries, func(e os.DirEntry) bool { return strings.HasPrefix(e.Name(), prefix) })
		if i >= 0 {
			existing, err := os.ReadFile(filepath.Join(dbDir, entries[i].Name()))
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(existing, mig.content) {
				return nil, fmt.Errorf("db/migrations/%s has version %d, which %s's migration %s uses with other content; gorbital.Migrate would refuse both: give the app's migration a new version first", entries[i].Name(), mig.version, m.pkg, mig.file)
			}
			res.Notes = append(res.Notes, fmt.Sprintf("db/migrations/%s is already %s's migration %s", entries[i].Name(), m.pkg, mig.file))
			continue
		}
		p := "db/migrations/" + name
		changes = append(changes, genplan.Change{Path: p, Kind: genplan.Create, Content: mig.content})
		res.Migrations = append(res.Migrations, p)
	}
	return changes, nil
}

type declaredMigration struct {
	version    int64
	name, file string
	content    []byte
}

// declaredMigrations reads the gorbital.Migration literals of the non-test
// files of the package at src, with their SQL from the embedded directory
// each one's FS names.
func declaredMigrations(libDir, src string, m ejectableModule) ([]declaredMigration, error) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	var out []declaredMigration
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(src, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		imports := map[string]string{}
		local := ""
		for _, spec := range file.Imports {
			imp, _ := strconv.Unquote(spec.Path.Value)
			name := path.Base(imp)
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = imp
			if imp == gorbitalImportPath {
				local = name
			}
		}
		if local == "" {
			continue
		}
		isMigration := func(t ast.Expr) bool {
			sel, ok := t.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Migration" {
				return false
			}
			x, ok := sel.X.(*ast.Ident)
			return ok && x.Name == local
		}
		var parseErr error
		read := func(lit *ast.CompositeLit) {
			mig, err := readMigrationLiteral(lit, imports, libDir, m)
			if err != nil {
				parseErr = fmt.Errorf("%s/%s:%d: %w", m.importPath(), e.Name(), fset.Position(lit.Pos()).Line, err)
				return
			}
			out = append(out, mig)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			switch {
			case parseErr != nil:
				return false
			case !ok:
				return true
			case isMigration(lit.Type):
				read(lit)
			case lit.Type != nil:
				// A []gorbital.Migration literal whose elements leave out
				// their type.
				if arr, ok := lit.Type.(*ast.ArrayType); ok && isMigration(arr.Elt) {
					for _, elt := range lit.Elts {
						if el, ok := elt.(*ast.CompositeLit); ok && el.Type == nil {
							read(el)
						}
					}
				}
			}
			return parseErr == nil
		})
		if parseErr != nil {
			return nil, parseErr
		}
	}
	slices.SortFunc(out, func(a, b declaredMigration) int { return int(a.version - b.version) })
	return out, nil
}

// readMigrationLiteral reads a gorbital.Migration literal whose Version,
// Name and File are literals and whose FS is an embedded FS of the module.
func readMigrationLiteral(lit *ast.CompositeLit, imports map[string]string, libDir string, m ejectableModule) (declaredMigration, error) {
	var mig declaredMigration
	var fsImport string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return mig, errors.New("copying a module reads gorbital.Migration literals with field names")
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return mig, errors.New("copying a module reads gorbital.Migration literals with field names")
		}
		switch key.Name {
		case "Version":
			if b, ok := kv.Value.(*ast.BasicLit); ok && b.Kind == token.INT {
				mig.version, _ = strconv.ParseInt(b.Value, 0, 64)
			}
		case "Name", "File":
			if b, ok := kv.Value.(*ast.BasicLit); ok && b.Kind == token.STRING {
				s, _ := strconv.Unquote(b.Value)
				if key.Name == "Name" {
					mig.name = s
				} else {
					mig.file = s
				}
			}
		case "FS":
			if sel, ok := kv.Value.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok {
					fsImport = imports[x.Name]
				}
			}
		}
	}
	rest, inModule := strings.CutPrefix(fsImport, m.importPath()+"/")
	if mig.version <= 0 || mig.name == "" || mig.file == "" || !inModule || !fs.ValidPath(mig.file) {
		return mig, errors.New("only migrations declared with a literal Version, Name and File and an FS of the module's own package are copied")
	}
	content, err := os.ReadFile(filepath.Join(libDir, m.pkg, filepath.FromSlash(rest), filepath.FromSlash(mig.file))) //nolint:gosec // the library's source
	if err != nil {
		return mig, fmt.Errorf("read the migration %s: %w", mig.file, err)
	}
	mig.content = content
	return mig, nil
}

// appGoFiles returns the app's Go files, slash-separated and relative to
// dir, leaving out directories the go command ignores and nested modules.
func appGoFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != dir && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" || name == "node_modules") {
				return filepath.SkipDir
			}
			if p != dir {
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	return files, err
}

// rewriteAppImports plans the app's files that import the module's library
// package, importing the copy instead.
func rewriteAppImports(app appInfo, m ejectableModule, rewrite func(string) (string, string, bool)) ([]genplan.Change, error) {
	files, err := appGoFiles(app.dir)
	if err != nil {
		return nil, err
	}
	only := func(imp string) (string, string, bool) {
		if imp != m.importPath() && !strings.HasPrefix(imp, m.importPath()+"/") {
			return "", "", false
		}
		return rewrite(imp)
	}
	var changes []genplan.Change
	for _, f := range files {
		before, err := os.ReadFile(filepath.Join(app.dir, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		if !bytes.Contains(before, []byte(m.importPath())) {
			continue
		}
		after, changed, err := rewriteGoImports(f, before, only)
		if err != nil {
			return nil, err
		}
		if changed {
			changes = append(changes, genplan.Change{Path: f, Kind: genplan.Modify, Before: before, Content: after})
		}
	}
	return changes, nil
}

// commandImports returns the non-test Go files of cmd/api that import imp,
// with the name each imports it under.
func commandImports(dir, imp string) (map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "cmd", "api", "*.go"))
	if err != nil {
		return nil, err
	}
	found := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		for _, spec := range file.Imports {
			if p, _ := strconv.Unquote(spec.Path.Value); p == imp {
				name := path.Base(imp)
				if spec.Name != nil {
					name = spec.Name.Name
				}
				found["cmd/api/"+filepath.Base(f)] = name
			}
		}
	}
	return found, nil
}

// commandSelectors reports whether a non-test file of cmd/api that imports
// imp under a usable name refers to name in it, such as authhttp.New.
func commandSelectors(dir, imp, name string) (bool, error) {
	files, err := filepath.Glob(filepath.Join(dir, "cmd", "api", "*.go"))
	if err != nil {
		return false, err
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.SkipObjectResolution)
		if err != nil {
			return false, err
		}
		local := ""
		for _, spec := range file.Imports {
			if p, _ := strconv.Unquote(spec.Path.Value); p == imp {
				local = path.Base(imp)
				if spec.Name != nil {
					local = spec.Name.Name
				}
			}
		}
		if local == "" || local == "_" || local == "." {
			continue
		}
		found := false
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == name {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == local {
					found = true
				}
			}
			return !found
		})
		if found {
			return true, nil
		}
	}
	return false, nil
}

// checkModuleUse refuses a module cmd/api doesn't add, or adds in a way
// orb eject can't follow by changing imports.
func checkModuleUse(dir string, m ejectableModule) error {
	uses, err := commandImports(dir, m.importPath())
	if err != nil {
		return err
	}
	if len(uses) == 0 {
		return fmt.Errorf("the app doesn't use %s: no file of cmd/api imports %s, so there is nothing to copy", m.pkg, m.importPath())
	}
	for file, name := range uses {
		if name == "_" || name == "." {
			return fmt.Errorf("%s imports %s as %q, which the copy can't follow; import it by name and call %s.%s, as a new app's main.go does", file, m.importPath(), name, m.pkg, m.constructor)
		}
	}
	calls, err := commandSelectors(dir, m.importPath(), m.constructor)
	if err != nil {
		return err
	}
	if !calls {
		return fmt.Errorf("cmd/api imports %s but never calls %s.%s, which the copy keeps while it changes the import; add the module with %s.%s in main.go, as a new app's does", m.importPath(), m.pkg, m.constructor, m.pkg, m.constructor)
	}
	return nil
}
