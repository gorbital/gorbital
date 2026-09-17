package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
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

	"github.com/charmbracelet/huh"

	"gorbital.dev/cli/internal/genplan"
)

const ejectUsage = `Usage: orb eject <module> [flags]

Copies a built-in module of gorbital.dev/gorbital into the app as code the
app owns, for changes no option or hook covers (ADR-0083). Modules:

  auth         sign-in (gorbital.dev/gorbital/authhttp)
  flags        client feature flags (gorbital.dev/gorbital/flagshttp)
  mailevents   email events (gorbital.dev/gorbital/mailevents)
  ops          the operations API (gorbital.dev/gorbital/opshttp)
  orgs         organisations (gorbital.dev/gorbital/orgshttp)

In an app on gorbital.Main, orb eject:

  1. copies the package, at the gorbital.dev/gorbital version go.mod requires
     (or its replace directive's directory), into internal/modules/<module>:
     the root package and its layers, internal/<layer> becoming <layer>,
     with their tests and import paths changed to the app's; tests of the
     library itself (marked //orb:noeject) aren't copied
  2. changes the app's imports of the package, so cmd/api/main.go builds the
     module from the copy with the same options and hooks; the package keeps
     its name, so no call changes
  3. copies the module's migrations into db/migrations under the same
     versions, so a database sees nothing new
  4. records the ejection in gorbital.lock, then runs go mod tidy

The API, database and behaviour are unchanged. From then on library releases
don't change the module; orb doctor says when the library's copy changes.
Eject orgs before auth: gorbital's orgshttp takes sign-in's authenticator.

Exit codes: 0 when the module is ejected (or would be, with --dry-run), 1
when orb eject refuses or fails, 2 for invalid usage, 130 when cancelled.
`

// An ejectableModule is a built-in module orb eject copies into apps.
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

func ejectableNames() string {
	names := make([]string, len(ejectableModules))
	for i, m := range ejectableModules {
		names[i] = m.name
	}
	return strings.Join(names, ", ")
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

func runEject(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb eject", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dryRun := flags.Bool("dry-run", false, "show what would change without writing")
	diff := flags.Bool("diff", false, "print the plan as a unified diff")
	asJSON := flags.Bool("json", false, "print the result as JSON")
	allowDirty := flags.Bool("allow-dirty", false, "allow uncommitted changes in the git repository")
	skipTidy := flags.Bool("skip-tidy", false, "don't run go mod tidy")
	var p promptFlags
	flags.BoolVar(&p.yes, "yes", false, "eject without asking for confirmation")
	flags.BoolVar(&p.noInput, "no-input", false, "never prompt; fail if the module isn't given")
	flags.BoolVar(&p.plain, "plain", false, "plain line-by-line prompts (screen-reader friendly)")
	flags.Usage = func() {
		fmt.Fprint(stderr, ejectUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	positional, err := parseInterspersed(flags, args)
	if err != nil {
		return err
	}
	if len(positional) > 1 {
		return usageError("orb eject takes one module: " + ejectableNames())
	}
	app, err := findApp()
	if err != nil {
		return err
	}
	ask := shouldPrompt(p, *asJSON, stdin, stdout)
	name := ""
	if len(positional) == 1 {
		name = positional[0]
	}
	if name == "" {
		if !ask {
			return usageError("orb eject needs a module: " + ejectableNames())
		}
		if name, err = promptEjectModule(app, p, stdin, stderr); err != nil {
			return err
		}
	}
	if _, ok := lookupEjectable(name); !ok {
		return usageError(fmt.Sprintf("orb eject can't eject %q; the built-in modules are %s", name, ejectableNames()))
	}

	plan, err := planEject(ctx, app, name, time.Now())
	if err != nil {
		return err
	}
	result := plan.Result.(ejectResult)
	result.DryRun = *dryRun
	if !*dryRun {
		if ask && !p.yes {
			ok, err := confirm("Eject "+name+"? The app owns the copy from then on.", plan.Summary, p, stdin, stderr)
			if err != nil {
				return err
			}
			if !ok {
				return errAborted
			}
		}
		if !*allowDirty {
			if err := requireCleanGit(ctx, app.dir); err != nil {
				return err
			}
		}
		if err := genplan.Apply(app.dir, plan); err != nil {
			return err
		}
		if !*skipTidy {
			var out bytes.Buffer
			if err := runIn(ctx, app.dir, &out, "go", "mod", "tidy"); err != nil {
				return fmt.Errorf("the module is ejected, but go mod tidy failed: %w: %s", err, strings.TrimSpace(out.String()))
			}
			result.Tidied = true
		}
		// The module's error codes and audit actions are the app's names
		// now, so the app's own record of them is written again (ADR-0054).
		recorded, err := recordSurface(ctx, app.dir)
		if err != nil {
			return fmt.Errorf("the module is ejected, but recording %s failed: %w", surfacePath, err)
		}
		result.Surface = recorded
	}

	if *asJSON {
		return writeJSON(stdout, result)
	}
	verb := "Ejected"
	if *dryRun {
		verb = "Would eject (dry run)"
	}
	fmt.Fprintf(stdout, "✓ %s %s: %s %s is now %s\n\n%s", verb, name, result.Package, result.Version, result.Directory, plan.Summary)
	if *diff {
		fmt.Fprintf(stdout, "\n%s", genplan.Diff(plan))
	}
	if !*dryRun {
		steps := plan.Next
		if *skipTidy {
			steps = append([]string{"go mod tidy"}, steps...)
		}
		if result.Surface {
			fmt.Fprintf(stdout, "  %s records the module's names, which are yours now\n", surfacePath)
		}
		fmt.Fprintf(stdout, "\nNext:\n")
		for _, s := range steps {
			fmt.Fprintf(stdout, "  %s\n", s)
		}
		fmt.Fprintf(stdout, "\nThe code is yours: library releases no longer change it, and orb doctor says when the library's %s changes.\n", path.Base(result.Package))
	}
	return nil
}

// promptEjectModule asks which of the modules cmd/api uses to eject.
func promptEjectModule(app appInfo, p promptFlags, stdin io.Reader, stderr io.Writer) (string, error) {
	lock, _ := readLock(app.dir)
	var options []huh.Option[string]
	for _, m := range ejectableModules {
		if _, done := lock.ejected(m.name); done {
			continue
		}
		if uses, _ := commandImports(app.dir, m.importPath()); len(uses) > 0 {
			options = append(options, huh.NewOption(fmt.Sprintf("%s: %s (%s)", m.name, m.about, m.importPath()), m.name))
		}
	}
	if len(options) == 0 {
		return "", errors.New("cmd/api uses no built-in module orb eject copies (" + ejectableNames() + ")")
	}
	name := options[0].Value
	form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Which module should the app own?").
		Description("The copy stops receiving library fixes; prefer options and hooks when they cover the change.").
		Options(options...).Value(&name)))
	return name, runForm(form, p, stdin, stderr)
}

// planEject returns the plan of ejecting the module named name from the app,
// refusing an app that isn't on gorbital.Main, a module the app doesn't use
// or already ejected, and a module a library package the app uses depends
// on. The plan's Result is an ejectResult.
func planEject(ctx context.Context, app appInfo, name string, now time.Time) (genplan.Plan, error) {
	m, ok := lookupEjectable(name)
	if !ok {
		return genplan.Plan{}, usageError(fmt.Sprintf("orb eject can't eject %q; the built-in modules are %s", name, ejectableNames()))
	}
	switch appLayout(app.dir) {
	case layoutV01:
		return genplan.Plan{}, fmt.Errorf("this app uses the v0.1 layout (internal/app), where %s is generated code the app already owns; orb eject works in apps on gorbital.Main: convert the app with orb upgrade --layout v0.2, which keeps a changed module as owned code", m.dir())
	case layoutMain:
	default:
		return genplan.Plan{}, errors.New("orb eject works in apps on gorbital.Main: go.mod doesn't require gorbital.dev/gorbital")
	}
	if !callsGorbitalMain(app.dir) {
		return genplan.Plan{}, errors.New("orb eject works in apps on gorbital.Main: no Go file in cmd/api calls gorbital.Main")
	}

	lock, err := readLock(app.dir)
	lockExists := err == nil
	if err != nil && !errors.Is(err, errNoLock) {
		return genplan.Plan{}, err
	}
	if lockExists && lock.APIVersion != LockAPIVersion {
		return genplan.Plan{}, errors.New("gorbital.lock was written by an early development build of orb; run orb upgrade --from <commit that created the app> first")
	}
	if e, done := lock.ejected(m.name); done {
		return genplan.Plan{}, fmt.Errorf("%s is already ejected: %s is the app's code, copied from %s %s on %s", m.name, m.dir(), e.Package, e.Version, e.Date)
	}
	if _, err := os.Stat(filepath.Join(app.dir, filepath.FromSlash(m.dir()))); err == nil {
		return genplan.Plan{}, fmt.Errorf("%s already exists; move it away to eject %s", m.dir(), m.name)
	}
	if err := checkModuleUse(app.dir, m); err != nil {
		return genplan.Plan{}, err
	}
	if err := checkLibraryDependents(ctx, app.dir, m); err != nil {
		return genplan.Plan{}, err
	}
	lib, err := resolveLibrary(ctx, app.dir)
	if err != nil {
		return genplan.Plan{}, err
	}
	return planEjectFrom(app, m, lib, lock, lockExists, now)
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
		return genplan.Plan{}, fmt.Errorf("%s %s has no %s package; orb eject copies modules from v0.2.0 on", gorbitalImportPath, lib.Version, m.pkg)
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

// rewriteGoImports changes src's imports that rewrite maps, keeping
// everything else byte for byte, then formats it. An import whose package
// name differs from its new path's last element gets the name, so the file
// keeps compiling and reads the same.
func rewriteGoImports(filename string, src []byte, rewrite func(string) (string, string, bool)) ([]byte, bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", filename, err)
	}
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for _, spec := range file.Imports {
		imp, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		to, name, ok := rewrite(imp)
		if !ok {
			continue
		}
		text := strconv.Quote(to)
		if spec.Name == nil && name != "" && name != path.Base(to) {
			text = name + " " + text
		}
		edits = append(edits, edit{fset.Position(spec.Path.Pos()).Offset, fset.Position(spec.Path.End()).Offset, text})
	}
	if len(edits) == 0 {
		return src, false, nil
	}
	out := slices.Clone(src)
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		out = slices.Concat(out[:e.start], []byte(e.text), out[e.end:])
	}
	formatted, err := format.Source(out)
	if err != nil {
		return nil, false, fmt.Errorf("format %s: %w", filename, err)
	}
	return formatted, true, nil
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
			return mig, errors.New("orb eject reads gorbital.Migration literals with field names")
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			return mig, errors.New("orb eject reads gorbital.Migration literals with field names")
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
		return mig, errors.New("orb eject copies migrations declared with a literal Version, Name and File and an FS of the module's own package")
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

// callsGorbitalMain reports whether a non-test file of cmd/api calls
// gorbital.Main.
func callsGorbitalMain(dir string) bool {
	calls, _ := commandSelectors(dir, gorbitalImportPath, "Main")
	return calls
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
		return fmt.Errorf("the app doesn't use %s: no file of cmd/api imports %s, so there is nothing to eject", m.pkg, m.importPath())
	}
	for file, name := range uses {
		if name == "_" || name == "." {
			return fmt.Errorf("%s imports %s as %q, which orb eject can't follow; import it by name and call %s.%s, as a new app's main.go does, then run orb eject %s again", file, m.importPath(), name, m.pkg, m.constructor, m.name)
		}
	}
	calls, err := commandSelectors(dir, m.importPath(), m.constructor)
	if err != nil {
		return err
	}
	if !calls {
		return fmt.Errorf("cmd/api imports %s but never calls %s.%s, which orb eject keeps while it changes the import; add the module with %s.%s in main.go, as a new app's does, then run orb eject %s again", m.importPath(), m.pkg, m.constructor, m.pkg, m.constructor, m.name)
	}
	return nil
}

// checkLibraryDependents refuses to eject a module another library package
// the app builds with imports, such as orgshttp, which takes sign-in's
// authenticator: the app's copy would have other types than the ones that
// package expects.
func checkLibraryDependents(ctx context.Context, dir string, m ejectableModule) error {
	var stderr bytes.Buffer
	out, err := goOutputIn(ctx, dir, &stderr, "list", "-deps", "-json=ImportPath,Imports", "./...")
	if err != nil {
		return fmt.Errorf("list the app's packages: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var dependents []string
	for {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := dec.Decode(&pkg); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("read go list's output: %w", err)
		}
		inLibrary := strings.HasPrefix(pkg.ImportPath, gorbitalImportPath+"/")
		own := pkg.ImportPath == m.importPath() || strings.HasPrefix(pkg.ImportPath, m.importPath()+"/")
		if inLibrary && !own && slices.Contains(pkg.Imports, m.importPath()) {
			dependents = append(dependents, pkg.ImportPath)
		}
	}
	if len(dependents) == 0 {
		return nil
	}
	slices.Sort(dependents)
	var first []string
	for _, d := range dependents {
		if other, ok := ejectableFor(d); ok {
			first = append(first, "orb eject "+other.name)
		}
	}
	fix := "those packages need the library's " + m.pkg
	if len(first) > 0 {
		fix = "eject first: " + strings.Join(first, ", ")
	}
	return fmt.Errorf("the app uses %s from the library, which imports %s and takes its types, so the app's copy wouldn't fit it; %s", strings.Join(dependents, ", "), m.importPath(), fix)
}

// ejectableFor returns the ejectable module a library package belongs to.
func ejectableFor(imp string) (ejectableModule, bool) {
	for _, m := range ejectableModules {
		if imp == m.importPath() || strings.HasPrefix(imp, m.importPath()+"/") {
			return m, true
		}
	}
	return ejectableModule{}, false
}
