package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The checks orb doctor runs in apps on gorbital.Main (ADR-0083): the module
// list, ejected modules, the middleware stack and the request timeout.

// modules checks internal/modules/modules.gen.go against the module
// directories, and reports directories that declare no module.
func (d *doctor) modules(app appInfo) {
	root := d.path("internal/modules")
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		d.add(doctorWarn, "modules", "the app has no internal/modules directory", "create a module with orb gen module")
		return
	} else if err != nil {
		d.add(doctorFail, "modules", firstLine(err.Error()), "")
		return
	}
	plan, err := planModules(app)
	if err != nil {
		d.add(doctorFail, "modules", firstLine(err.Error()), "fix the Go file so it parses; orb gen modules reads every module")
		return
	}
	found := plan.Result.(genModulesResult).Modules

	// Directories with Go code but no func Module() aren't in the app. A
	// Module that takes arguments, such as the authenticator, isn't listed
	// either: main.go adds it on its own line (ADR-0083).
	lock, _ := readLock(app.dir)
	for _, e := range entries {
		name := e.Name()
		if _, ejected := lock.ejected(name); !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || slices.Contains(found, name) || ejected {
			continue
		}
		if _, kind, err := moduleDeclaration(filepath.Join(root, name)); err == nil && kind == moduleWithArgs {
			continue
		}
		if hasGoFiles(filepath.Join(root, name)) {
			d.add(doctorWarn, "modules", fmt.Sprintf("internal/modules/%s has Go files but no func Module() gorbital.Module, so it isn't part of the app", name),
				"declare func Module() gorbital.Module in its module.go, as orb gen module writes it, then run orb gen modules")
		}
	}

	listed, err := listedModules(d.path(modulesGenPath), app.module)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		d.add(doctorFail, "modules", fmt.Sprintf("%s is missing, so main.go can't list the app's modules (%s)", modulesGenPath, moduleCount(found)), "orb gen modules (orb dev runs it)")
	case err != nil:
		d.add(doctorFail, "modules", modulesGenPath+" doesn't parse: "+firstLine(err.Error()), "orb gen modules rewrites it")
	case len(plan.Changes) > 0:
		var problems []string
		for _, m := range found {
			if !slices.Contains(listed, m) {
				problems = append(problems, m+" isn't listed")
			}
		}
		for _, m := range listed {
			if !slices.Contains(found, m) {
				problems = append(problems, m+" is listed but has no module")
			}
		}
		if len(problems) == 0 {
			problems = append(problems, "it isn't what orb gen modules writes")
		}
		d.add(doctorFail, "modules", modulesGenPath+" is stale: "+strings.Join(problems, "; "), "orb gen modules (orb dev runs it before each build)")
	default:
		d.add(doctorOK, "modules", modulesGenPath+" lists "+moduleCount(found), "")
	}
}

// hasGoFiles reports whether dir, or a directory below it, has a Go file
// other than a test.
func hasGoFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// listedModules returns the module directories modules.gen.go imports.
func listedModules(path, module string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if dir, ok := strings.CutPrefix(p, module+"/internal/modules/"); ok {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

// stackStep is a built-in step orb doctor looks for in a custom stack.
type stackStep struct {
	field, without string
}

var requiredStackSteps = []stackStep{
	{"Recover", "a panic in a handler ends the connection instead of answering 500"},
	{"Auth", "no request is authenticated, so only public routes succeed"},
}

// stack checks a gorbital.WithStack in cmd/api for the Recover and Auth
// steps, when the function is written in place or declared in the package.
func (d *doctor) stack() {
	fset := token.NewFileSet()
	files, _ := filepath.Glob(d.path("cmd/api/*.go"))
	var parsed []*ast.File
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution); err == nil {
			parsed = append(parsed, file)
		}
	}
	funcs := map[string]*ast.FuncType{}
	bodies := map[string]*ast.BlockStmt{}
	for _, file := range parsed {
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
				funcs[fn.Name.Name], bodies[fn.Name.Name] = fn.Type, fn.Body
			}
		}
	}
	checked := false
	for _, file := range parsed {
		local := ""
		for _, imp := range file.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == gorbitalImportPath {
				local = "gorbital"
				if imp.Name != nil {
					local = imp.Name.Name
				}
			}
		}
		if local == "" {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "WithStack" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != local {
				return true
			}
			checked = true
			rel, _ := filepath.Rel(d.dir, fset.Position(call.Pos()).Filename)
			where := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), fset.Position(call.Pos()).Line)
			var fnType *ast.FuncType
			var body *ast.BlockStmt
			switch arg := call.Args[0].(type) {
			case *ast.FuncLit:
				fnType, body = arg.Type, arg.Body
			case *ast.Ident:
				fnType, body = funcs[arg.Name], bodies[arg.Name]
			}
			if body == nil || len(fnType.Params.List) != 1 || len(fnType.Params.List[0].Names) != 1 {
				d.add(doctorWarn, "stack", "the custom stack at "+where+" can't be checked without running the app", "gorbital.New logs a warning at start when the stack leaves out Recover or Auth; look for it in the app's output")
				return true
			}
			d.checkStack(where, fnType.Params.List[0].Names[0].Name, body)
			return true
		})
	}
	if !checked {
		d.add(doctorOK, "stack", "the default middleware stack", "")
	}
}

// checkStack reports the required steps the stack function's body never
// names on its parameter param, unless it uses param.Default().
func (d *doctor) checkStack(where, param string, body *ast.BlockStmt) {
	used := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == param {
				used[sel.Sel.Name] = true
			}
		}
		return true
	})
	if used["Default"] {
		d.add(doctorOK, "stack", "the custom stack at "+where+" builds on the default one", "")
		return
	}
	var missing []string
	for _, step := range requiredStackSteps {
		if !used[step.field] {
			missing = append(missing, fmt.Sprintf("%s (%s)", step.field, step.without))
		}
	}
	if len(missing) == 0 {
		d.add(doctorOK, "stack", "the custom stack at "+where+" keeps Recover and Auth", "")
		return
	}
	d.add(doctorWarn, "stack", "the custom stack at "+where+" leaves out "+strings.Join(missing, " and "),
		"keep "+param+".Recover first and "+param+".Auth after "+param+".Maintenance, as Stack.Default does (docs/guides/middleware-stack.md)")
}

// maxRequestTimeout is the server's write timeout (httpx.DefaultWriteTimeout):
// gorbital.LoadConfig refuses a request timeout that long, which couldn't
// send its 503.
const maxRequestTimeout = 60 * time.Second

// requestTimeout checks APP_REQUEST_TIMEOUT from the environment and .env
// as gorbital.LoadConfig reads it.
func (d *doctor) requestTimeout(env []string) {
	const name = "APP_REQUEST_TIMEOUT"
	v := envValue(env, name, "")
	if v == "" {
		d.add(doctorOK, "timeout", "30s, the default ("+name+")", "")
		return
	}
	timeout, err := time.ParseDuration(v)
	switch {
	case err != nil || timeout < 0:
		d.add(doctorFail, "timeout", fmt.Sprintf("%s=%q isn't a duration, so the app refuses to start", name, v), "set it to a duration such as 30s, or 0 to turn the timeout off")
	case timeout >= maxRequestTimeout:
		d.add(doctorFail, "timeout", fmt.Sprintf("%s=%s isn't shorter than the server's %s write timeout, so the app refuses to start", name, timeout, maxRequestTimeout), "use less than 60s; give slow routes their own gorbital.Timeout")
	case timeout == 0:
		d.add(doctorWarn, "timeout", name+"=0 turns the timeout off: a slow query holds its request until the server's write timeout", "leave it unset (30s), and lengthen single routes with gorbital.Timeout")
	case timeout < time.Second:
		d.add(doctorWarn, "timeout", fmt.Sprintf("%s=%s: requests that take longer answer 503 request_timeout", name, timeout), "most apps use 10s to 30s")
	default:
		d.add(doctorOK, "timeout", fmt.Sprintf("%s (%s)", timeout, name), "")
	}
}

// ejected checks the built-in modules gorbital.lock records as ejected: the
// app has their code, and whether the library's package changed since, so
// the app may be missing fixes (orb eject).
func (d *doctor) ejected(ctx context.Context) {
	lock, err := readLock(d.dir)
	if err != nil || len(lock.Ejected) == 0 {
		return
	}
	lib, libErr := doctorModule(ctx, d.dir, gorbitalImportPath)
	for _, e := range lock.Ejected {
		m, _ := lookupEjectable(e.Module)
		if info, err := os.Stat(d.path(m.dir())); err != nil || !info.IsDir() {
			d.add(doctorFail, "ejected", fmt.Sprintf("gorbital.lock records %s as ejected, but %s is missing, so the app doesn't build", m.name, m.dir()),
				fmt.Sprintf("restore it from git history, or go back to the library's %s: remove the entry from gorbital.lock and change the imports back (docs/guides/ejecting-a-module.md)", m.pkg))
			continue
		}
		since := fmt.Sprintf("%s is the app's code, ejected from %s %s on %s", m.dir(), m.importPath(), e.Version, e.Date)
		if libErr != nil {
			d.add(doctorWarn, "ejected", since+"; couldn't compare it with the library: "+firstLine(libErr.Error()), "check that go.mod requires gorbital.dev/gorbital and go mod download works")
			continue
		}
		hash, err := hashLibraryPackage(filepath.Join(lib.Dir, m.pkg))
		switch {
		case err != nil:
			d.add(doctorWarn, "ejected", fmt.Sprintf("%s; gorbital.dev/gorbital %s has no %s to compare it with", since, lib.Version, m.pkg), "")
		case hash == e.SHA256:
			d.add(doctorOK, "ejected", since+"; the library's copy hasn't changed since", "")
		default:
			detail := fmt.Sprintf("%s; the library's %s has changed since (the app requires %s)", since, m.pkg, lib.Version)
			if entries := d.changelogEntries(ctx, m.pkg, e.Version); len(entries) > 0 {
				detail += ". Changelog: " + strings.Join(entries, " · ")
			}
			d.add(doctorWarn, "ejected", detail,
				fmt.Sprintf("compare %s with %s in gorbital.dev/gorbital %s and port the fixes you need: library releases and orb upgrade don't change ejected modules", m.dir(), m.pkg, lib.Version))
		}
	}
}

// doctorModule returns the version and source directory of a module the
// app requires, as go list reports them.
func doctorModule(ctx context.Context, dir, module string) (librarySource, error) {
	out, errOut, err := doctorCommand(ctx, dir, nil, "go", "list", "-m", "-json", module)
	if err != nil {
		return librarySource{}, fmt.Errorf("go list -m %s: %w: %s", module, err, firstLine(errOut))
	}
	var mod struct{ Version, Dir string }
	if err := json.Unmarshal([]byte(out), &mod); err != nil {
		return librarySource{}, err
	}
	if mod.Dir == "" {
		return librarySource{}, fmt.Errorf("%s %s isn't in the module cache; run go mod download", module, mod.Version)
	}
	return librarySource{Version: mod.Version, Dir: mod.Dir}, nil
}

// maxChangelogEntries is how many changelog entries orb doctor quotes for
// an ejected module.
const maxChangelogEntries = 3

// changelogEntries returns the first line of the changelog entries naming
// pkg in releases after version, from gorbital.dev's CHANGELOG.md at the
// version the app requires; none when it can't be read.
func (d *doctor) changelogEntries(ctx context.Context, pkg, version string) []string {
	root, err := doctorModule(ctx, d.dir, "gorbital.dev")
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(root.Dir, "CHANGELOG.md"))
	if err != nil {
		return nil
	}
	return changelogMentions(string(data), pkg, version)
}

// changelogMentions returns the entries of changelog, a Keep a Changelog
// file with "## v0.2.1 (date)" or "## Unreleased (v0.2.1)" headings, that
// name pkg in sections for versions after version, newest first.
func changelogMentions(changelog, pkg, version string) []string {
	var found []string
	newer := false
	total := 0
	for line := range strings.Lines(changelog) {
		line = strings.TrimRight(line, "\r\n")
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			v := changelogVersion.FindString(heading)
			newer = v != "" && v != version && versionAtLeast(strings.TrimPrefix(v, "v"), strings.TrimPrefix(version, "v"))
			continue
		}
		entry, ok := strings.CutPrefix(strings.TrimLeft(line, " "), "- ")
		if !newer || !ok || !strings.Contains(entry, pkg) {
			continue
		}
		total++
		if len(found) < maxChangelogEntries {
			if first, _, cut := strings.Cut(entry, ". "); cut {
				entry = first
			}
			if r := []rune(entry); len(r) > 120 {
				entry = string(r[:119]) + "…"
			}
			found = append(found, entry)
		}
	}
	if total > len(found) {
		found = append(found, fmt.Sprintf("and %d more", total-len(found)))
	}
	return found
}

// changelogVersion finds a version in a changelog heading.
var changelogVersion = regexp.MustCompile(`v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?`)
