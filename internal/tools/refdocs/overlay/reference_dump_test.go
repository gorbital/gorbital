//go:build ignore

// This file isn't built into refdocs: refdocs adds it to a golden app's
// cmd/api package for one go test -overlay run (removing the build
// constraint above), so it reads the app's real declarations while the app
// carries no documentation code. It builds the app with main.go's options()
// on the test database through gorbitaltest, and reads what New built: the
// permission catalogs and job definitions through a module's Platform, the
// settings through Deps.

package main

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/jobs"
)

type refDump struct {
	ServiceName string       `json:"service_name"`
	Catalogs    []refCatalog `json:"catalogs"`
	Settings    []refSetting `json:"settings"`
	Jobs        []refJob     `json:"jobs"`
	Codes       []refCode    `json:"codes"`
	Actions     []refAction  `json:"actions"`
}

type refCatalog struct {
	Name        string          `json:"name"`
	Permissions []refPermission `json:"permissions"`
	Roles       []refRole       `json:"roles"`
}

type refPermission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type refRole struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	RequiresMFA bool     `json:"requires_mfa"`
}

type refSetting struct {
	Key             string          `json:"key"`
	Kind            string          `json:"kind"`
	Group           string          `json:"group"`
	Description     string          `json:"description"`
	Default         json.RawMessage `json:"default"`
	ReasonRequired  bool            `json:"reason_required"`
	RestartRequired bool            `json:"restart_required"`
	Constraints     map[string]any  `json:"constraints,omitempty"`
}

type refJob struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Definition  bool   `json:"definition"`
	Enabled     bool   `json:"enabled"`
	Schedule    string `json:"schedule"`
	Timeout     string `json:"timeout"`
	MaxAttempts int    `json:"max_attempts"`
	Queue       string `json:"queue"`
	Priority    int    `json:"priority"`
}

type refCode struct {
	Code     string `json:"code"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Generic  bool   `json:"generic,omitempty"`
	Location string `json:"location"`
	// Module names the library module an app must add to get this code,
	// for the codes of refExtraModules; empty for everything the app has.
	Module string `json:"module,omitempty"`
}

type refAction struct {
	Action   string   `json:"action"`
	Metadata []string `json:"metadata,omitempty"`
	Location string   `json:"location"`
}

func TestReferenceDump(t *testing.T) {
	out := os.Getenv("REFDOCS_OUT")
	if out == "" {
		t.Skip("run by gorbital's internal/tools/refdocs")
	}
	ctx := context.Background()
	var platform *gorbital.Platform
	capture := gorbital.Module{Name: "refdocs_dump", Platform: func(p *gorbital.Platform) error { platform = p; return nil }}
	app := gorbitaltest.New(t, append(options(), gorbital.WithModules(capture))...)
	if platform == nil {
		t.Fatal("the app built no Platform")
	}
	d := refDump{ServiceName: platform.Name}

	for _, c := range []struct {
		name    string
		catalog *auth.Catalog
	}{{"platform", platform.Permissions}, {"org", platform.OrgPermissions}} {
		if c.catalog == nil || len(c.catalog.AllPermissions()) == 0 {
			continue
		}
		rc := refCatalog{Name: c.name}
		for _, p := range c.catalog.AllPermissions() {
			rc.Permissions = append(rc.Permissions, refPermission{p.Name, p.Description})
		}
		for _, r := range c.catalog.Roles() {
			rc.Roles = append(rc.Roles, refRole{r.Name, r.Description, r.Permissions, c.catalog.RequiresMFA(r.Name)})
		}
		d.Catalogs = append(d.Catalogs, rc)
	}

	for _, v := range app.App().Deps().Settings.List() {
		d.Settings = append(d.Settings, refSetting{v.Key, string(v.Kind), v.Group, v.Description, v.Default, v.ReasonRequired, v.RestartRequired, v.Constraints})
	}
	defs, err := platform.Jobs.Definitions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range defs {
		c := v.Defaults
		d.Jobs = append(d.Jobs, refJob{v.Name, v.Description, true, c.Enabled, c.Schedule, c.Timeout.String(), c.MaxAttempts, c.Queue, c.Priority})
	}
	d.Jobs = append(d.Jobs, refJob{Name: jobs.MailKind})

	for status := 400; status <= 599; status++ {
		d.Codes = append(d.Codes, refCode{Code: httpx.DefaultCode(status), Status: status, Generic: true, Location: "gorbital.dev/httpx"})
	}
	scanReference(t, &d)

	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// refListFormat prints, per package, the fields scanPackages reads.
const refListFormat = `{{if .Module}}{{.Module.Path}}|{{.Module.Dir}}|{{.ImportPath}}|{{.Dir}}|{{join .GoFiles ","}}{{end}}`

// refExtraModules are library modules no golden app links, so the app's
// dependency graph below never reaches them and their error codes would be
// missing from the reference. Each is scanned on its own, from the checkout
// next to the app, and its codes are marked in the page as belonging to a
// module an app adds. Add a module here when it returns problem codes of its
// own and no golden app uses it.
var refExtraModules = []string{"gorbital.dev/modules/jwt"}

// scanReference finds error codes and audit actions the way
// TestPublicSurface does, keeping each one's status, detail, metadata keys
// and where it is written. It reads a code written as a string literal and
// one written as its own package's string constant; a code taken from a
// struct field or another package is invisible, so a code the library
// lets an app rename is also declared, with its default, where the
// default refusal is built.
func scanReference(t *testing.T, d *refDump) {
	t.Helper()
	statuses := httpStatuses(t)
	module := goOutput(t, "list", "-m")
	scanPackages(t, goOutput(t, "list", "-deps", "-f", refListFormat, "./..."), module, "", statuses, d)
	for _, extra := range refExtraModules {
		// The checkout's copy, not the published one: examples/<app> is two
		// directories below the root, and every gorbital.dev module lives
		// under it at its import path's tail.
		dir := filepath.Join("..", "..", filepath.FromSlash(strings.TrimPrefix(extra, "gorbital.dev/")))
		scanPackages(t, goOutput(t, "list", "-C", dir, "-f", refListFormat, "./..."), module, extra, statuses, d)
	}
}

// scanPackages parses every package of a go list run that belongs to the app
// or to the library, and records what it declares. With extra set, the
// packages come from a module the app doesn't link, and every code is marked
// with it.
func scanPackages(t *testing.T, list, module, extra string, statuses map[string]int, d *refDump) {
	t.Helper()
	before := len(d.Codes)
	for _, line := range strings.Split(list, "\n") {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) != 5 || parts[4] == "" {
			continue
		}
		mod, modDir, importPath, dir := parts[0], parts[1], parts[2], parts[3]
		if mod != module && mod != "gorbital.dev" && !strings.HasPrefix(mod, "gorbital.dev/") {
			continue
		}
		fset := token.NewFileSet()
		var files []*ast.File
		var names []string
		actionParams := map[string][]int{}
		actionConsts := map[string]string{} // constant name → action
		// Package-level string constants, so a code written as the
		// package's own constant is read like the literal it stands for:
		// gorbital.DefaultScopeNotFoundCode is org_not_found.
		stringConsts := map[string]string{}
		for _, name := range strings.Split(parts[4], ",") {
			f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
			names = append(names, name)
			for _, decl := range f.Decls {
				if gd, ok := decl.(*ast.GenDecl); ok && gd.Tok == token.CONST {
					for _, spec := range gd.Specs {
						vs := spec.(*ast.ValueSpec)
						for i, id := range vs.Names {
							lit, ok := valueAt(vs, i).(*ast.BasicLit)
							if !ok || lit.Kind != token.STRING {
								continue
							}
							value, err := strconv.Unquote(lit.Value)
							if err != nil {
								continue
							}
							stringConsts[id.Name] = value
							if strings.HasPrefix(id.Name, "Action") {
								actionConsts[id.Name] = value
							}
						}
					}
				}
				if fn, ok := decl.(*ast.FuncDecl); ok {
					i := 0
					for _, field := range fn.Type.Params.List {
						for _, n := range field.Names {
							if n.Name == "action" {
								actionParams[fn.Name.Name] = append(actionParams[fn.Name.Name], i)
							}
							i++
						}
						if len(field.Names) == 0 {
							i++
						}
					}
				}
			}
		}
		for i, f := range files {
			location := importPath
			if mod == module {
				rel, _ := filepath.Rel(modDir, filepath.Join(dir, names[i]))
				location = filepath.ToSlash(rel)
			}
			scanFile(f, location, statuses, actionParams, actionConsts, stringConsts, d)
		}
	}
	if extra != "" {
		for i := before; i < len(d.Codes); i++ {
			d.Codes[i].Module = extra
		}
	}
}

func scanFile(f *ast.File, location string, statuses map[string]int, actionParams map[string][]int, actionConsts, stringConsts map[string]string, d *refDump) {
	imports := map[string]bool{}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name] = true
	}
	literal := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		return s, err == nil
	}
	// codeName reads a problem code written as a string literal or as the
	// package's own string constant, so a code declared once and used by
	// name stays in the reference.
	codeName := func(e ast.Expr) (string, bool) {
		if s, ok := literal(e); ok {
			return s, true
		}
		if id, ok := e.(*ast.Ident); ok {
			s, ok := stringConsts[id.Name]
			return s, ok
		}
		return "", false
	}
	status := func(e ast.Expr) int {
		switch e := e.(type) {
		case *ast.SelectorExpr:
			if x, ok := e.X.(*ast.Ident); ok && x.Name == "http" {
				return statuses[e.Sel.Name]
			}
		case *ast.BasicLit:
			n, _ := strconv.Atoi(e.Value)
			return n
		}
		return 0
	}
	mapKeys := func(e ast.Expr) []string {
		lit, ok := e.(*ast.CompositeLit)
		if !ok {
			return nil
		}
		var keys []string
		for _, elt := range lit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if k, ok := literal(kv.Key); ok {
					keys = append(keys, k)
				}
			}
		}
		return keys
	}
	isAction := func(s string) bool { return refActionPattern.MatchString(s) }

	// Metadata keys set on a variable after it was built, such as
	// e := userEvent(...); e.Metadata["method"] = "password".
	laterKeys := func(body *ast.BlockStmt, variable string, from token.Pos) []string {
		var keys []string
		if body == nil || variable == "" {
			return nil
		}
		// Until the variable is assigned again.
		until := body.End()
		ast.Inspect(body, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok && as.Pos() > from && as.Pos() < until {
				for _, lhs := range as.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name == variable {
						until = as.Pos()
					}
				}
			}
			return true
		})
		ast.Inspect(body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || as.Pos() <= from || as.Pos() >= until {
				return true
			}
			for i, lhs := range as.Lhs {
				switch l := lhs.(type) {
				case *ast.IndexExpr:
					if sel, ok := l.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Metadata" {
						if x, ok := sel.X.(*ast.Ident); ok && x.Name == variable {
							if k, ok := literal(l.Index); ok {
								keys = append(keys, k)
							}
						}
					}
				case *ast.SelectorExpr:
					if x, ok := l.X.(*ast.Ident); ok && x.Name == variable && l.Sel.Name == "Metadata" && i < len(as.Rhs) {
						keys = append(keys, mapKeys(as.Rhs[i])...)
					}
				}
			}
			return true
		})
		return keys
	}

	for _, decl := range f.Decls {
		var body *ast.BlockStmt
		if fn, ok := decl.(*ast.FuncDecl); ok {
			body = fn.Body
		}
		assigned := map[ast.Node]string{} // expression → variable it is assigned to
		// Elements of a []httpx.Mapping literal, whose type is elided, such as
		// a built-in module's Errors.
		elided := map[*ast.CompositeLit]bool{}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
					if id, ok := n.Lhs[0].(*ast.Ident); ok {
						assigned[n.Rhs[0]] = id.Name
					}
				}
			case *ast.CompositeLit:
				if array, ok := n.Type.(*ast.ArrayType); ok && refIsName(array.Elt, "httpx", "Mapping", f.Name.Name) {
					for _, elt := range n.Elts {
						if lit, ok := elt.(*ast.CompositeLit); ok && lit.Type == nil {
							elided[lit] = true
						}
					}
				}
				mapping := refIsName(n.Type, "httpx", "Mapping", f.Name.Name) || elided[n]
				var code, detail, action string
				var st int
				var meta []string
				for _, elt := range n.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, _ := kv.Key.(*ast.Ident)
					if key == nil {
						continue
					}
					switch {
					case mapping && key.Name == "Code":
						code, _ = codeName(kv.Value)
					case mapping && key.Name == "Detail":
						detail, _ = literal(kv.Value)
					case mapping && key.Name == "Status":
						st = status(kv.Value)
					case key.Name == "Action":
						action, _ = literal(kv.Value)
					case key.Name == "Metadata":
						meta = mapKeys(kv.Value)
					}
				}
				if code != "" && refCodePattern.MatchString(code) {
					d.Codes = append(d.Codes, refCode{Code: code, Status: st, Detail: detail, Location: location})
				}
				if isAction(action) {
					d.Actions = append(d.Actions, refAction{Action: action, Metadata: append(meta, laterKeys(body, assigned[n], n.Pos())...), Location: location})
				}
			case *ast.CallExpr:
				if refIsName(n.Fun, "httpx", "NewProblem", f.Name.Name) && len(n.Args) == 3 {
					if code, ok := codeName(n.Args[1]); ok && refCodePattern.MatchString(code) {
						detail, _ := literal(n.Args[2])
						d.Codes = append(d.Codes, refCode{Code: code, Status: status(n.Args[0]), Detail: detail, Location: location})
					}
				}
				var name string
				switch fun := n.Fun.(type) {
				case *ast.Ident:
					name = fun.Name
				case *ast.SelectorExpr:
					if x, ok := fun.X.(*ast.Ident); !ok || !imports[x.Name] {
						name = fun.Sel.Name
					}
				}
				for _, i := range actionParams[name] {
					if i >= len(n.Args) {
						continue
					}
					action, ok := literal(n.Args[i])
					if id, isIdent := n.Args[i].(*ast.Ident); isIdent {
						action, ok = actionConsts[id.Name]
					}
					if ok && isAction(action) {
						var meta []string
						for _, arg := range n.Args {
							meta = append(meta, mapKeys(arg)...)
						}
						d.Actions = append(d.Actions, refAction{Action: action, Metadata: append(meta, laterKeys(body, assigned[n], n.Pos())...), Location: location})
					}
				}
			case *ast.GenDecl:
				if n.Tok != token.CONST {
					break
				}
				for _, spec := range n.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, id := range vs.Names {
						if strings.HasPrefix(id.Name, "Action") && i < len(vs.Values) {
							if action, ok := literal(vs.Values[i]); ok && isAction(action) {
								d.Actions = append(d.Actions, refAction{Action: action, Location: location})
							}
						}
					}
				}
			}
			return true
		})
	}
}

func valueAt(vs *ast.ValueSpec, i int) ast.Expr {
	if i < len(vs.Values) {
		return vs.Values[i]
	}
	return nil
}

// httpStatuses reads the net/http status constants from the Go source.
func httpStatuses(t *testing.T) map[string]int {
	t.Helper()
	path := filepath.Join(goOutput(t, "env", "GOROOT"), "src", "net", "http", "status.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]int{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, id := range vs.Names {
				if i < len(vs.Values) {
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok {
						n, _ := strconv.Atoi(lit.Value)
						statuses[id.Name] = n
					}
				}
			}
		}
	}
	return statuses
}

func goOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

var (
	refCodePattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	refActionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

// refIsName reports whether e names pkg.name, or name inside package pkg
// itself.
func refIsName(e ast.Expr, pkg, name, filePkg string) bool {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		x, ok := e.X.(*ast.Ident)
		return ok && x.Name == pkg && e.Sel.Name == name
	case *ast.Ident:
		return filePkg == pkg && e.Name == name
	}
	return false
}
