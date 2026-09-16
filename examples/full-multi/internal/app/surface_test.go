package app

import (
	"bytes"
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gorbital.dev/httpx"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/settings"
)

var updateSurface = flag.Bool("update", false, "record the current public surface in api/surface.json")

// surfacePath is the recorded public surface, relative to this package.
var surfacePath = filepath.Join("..", "..", "api", "surface.json")

// surface is the app's public surface besides its HTTP endpoints (ADR-0015,
// ADR-0054): names clients, operators, dashboards and stored data depend on.
// Every list is sorted.
type surface struct {
	// ErrorCodes are the problem+json codes the app and the library
	// packages it links can return.
	ErrorCodes []string `json:"error_codes"`
	// AuditActions are the actions of the audit events the app records.
	AuditActions []string `json:"audit_actions"`
	// Permissions and Roles are declared by each permission catalog, by
	// catalog name.
	Permissions map[string][]string `json:"permissions"`
	Roles       map[string][]string `json:"roles"`
	// Settings are the runtime setting keys.
	Settings []string `json:"settings"`
	// Jobs are the job definition names and the job kinds the app handles.
	Jobs []string `json:"jobs"`
}

// TestPublicSurface fails when a public name recorded in api/surface.json
// disappears (a breaking change for clients and operators) or when a new one
// isn't recorded yet. After adding endpoints, jobs, settings or permissions,
// record them with
//
//	go test ./internal/app -run TestPublicSurface -update
//
// and commit api/surface.json; review removed lines like any breaking
// change.
func TestPublicSurface(t *testing.T) {
	got := currentSurface(t)
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')

	if *updateSurface {
		if err := os.WriteFile(surfacePath, data, 0o644); err != nil { //nolint:gosec // committed to the repository
			t.Fatal(err)
		}
		return
	}
	recorded, err := os.ReadFile(surfacePath)
	if err != nil {
		t.Fatalf("read api/surface.json: %v; record it with: go test ./internal/app -run TestPublicSurface -update", err)
	}
	var want surface
	if err := json.Unmarshal(recorded, &want); err != nil {
		t.Fatalf("api/surface.json: %v", err)
	}

	for _, d := range []struct {
		name      string
		want, got []string
	}{
		{"error code", want.ErrorCodes, got.ErrorCodes},
		{"audit action", want.AuditActions, got.AuditActions},
		{"setting", want.Settings, got.Settings},
		{"job", want.Jobs, got.Jobs},
	} {
		compareNames(t, d.name, d.want, d.got)
	}
	for _, catalog := range slices.Sorted(maps.Keys(mergeKeys(want.Permissions, got.Permissions))) {
		compareNames(t, catalog+" permission", want.Permissions[catalog], got.Permissions[catalog])
	}
	for _, catalog := range slices.Sorted(maps.Keys(mergeKeys(want.Roles, got.Roles))) {
		compareNames(t, catalog+" role", want.Roles[catalog], got.Roles[catalog])
	}
	if !t.Failed() && !bytes.Equal(recorded, data) {
		t.Error("api/surface.json isn't formatted as recorded; run: go test ./internal/app -run TestPublicSurface -update")
	}
}

func compareNames(t *testing.T, kind string, want, got []string) {
	t.Helper()
	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Errorf("%s %q is recorded in api/surface.json but no longer exists; it is public API (ADR-0015): restore it, or remove it deliberately with -update and call it out as a breaking change", kind, name)
		}
	}
	for _, name := range got {
		if !slices.Contains(want, name) {
			t.Errorf("new %s %q isn't recorded; run: go test ./internal/app -run TestPublicSurface -update", kind, name)
		}
	}
}

func mergeKeys(a, b map[string][]string) map[string][]string {
	out := maps.Clone(a)
	if out == nil {
		out = map[string][]string{}
	}
	maps.Copy(out, b)
	return out
}

// currentSurface collects the surface from the declarations the app runs
// with, and error codes and audit actions from the source of the app and of
// every gorbital package it links.
func currentSurface(t *testing.T) surface {
	t.Helper()
	s := surface{Permissions: map[string][]string{}, Roles: map[string][]string{}}

	for name, c := range permissionCatalogs() {
		for _, p := range c.AllPermissions() {
			s.Permissions[name] = append(s.Permissions[name], p.Name)
		}
		for _, r := range c.Roles() {
			s.Roles[name] = append(s.Roles[name], r.Name)
		}
		slices.Sort(s.Permissions[name])
		slices.Sort(s.Roles[name])
	}

	reg := settings.NewRegistry()
	declareSettings(reg)
	s.Settings = slices.Sorted(slices.Values(reg.Keys()))

	defs := jobs.NewDefinitions()
	defineJobs(defs, jobDeps{})
	// The mail worker handles queued email under its own kind (app.go).
	s.Jobs = slices.Sorted(slices.Values(append(defs.Names(), jobs.MailKind)))

	codes, actions := map[string]bool{}, map[string]bool{}
	for status := 400; status <= 599; status++ {
		codes[httpx.DefaultCode(status)] = true // Huma's validation and other client errors
	}
	for _, pkg := range linkedPackages(t) {
		scanPackage(t, pkg, codes, actions)
	}
	s.ErrorCodes = slices.Sorted(maps.Keys(codes))
	s.AuditActions = slices.Sorted(maps.Keys(actions))
	return s
}

// linkedPackages returns the Go files of every package of this module and
// every gorbital.dev package the module links, by package.
func linkedPackages(t *testing.T) [][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-f", `{{if .Module}}{{.Module.Path}}|{{.Dir}}|{{join .GoFiles ","}}{{end}}`, "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	mod := exec.Command("go", "list", "-m")
	mod.Dir = cmd.Dir
	module, err := mod.Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	var pkgs [][]string
	for line := range strings.Lines(string(out)) {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) != 3 || parts[2] == "" {
			continue
		}
		if parts[0] != strings.TrimSpace(string(module)) && parts[0] != "gorbital.dev" && !strings.HasPrefix(parts[0], "gorbital.dev/") {
			continue
		}
		var files []string
		for _, f := range strings.Split(parts[2], ",") {
			files = append(files, filepath.Join(parts[1], f))
		}
		pkgs = append(pkgs, files)
	}
	if len(pkgs) == 0 {
		t.Fatal("go list found no packages")
	}
	return pkgs
}

var (
	codePattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

// scanPackage adds the error codes and audit actions written in a package's
// files. It recognises how gorbital code writes them, as string literals:
//
//   - error codes in httpx.Mapping{Code: "..."} and httpx.NewProblem(status, "...", detail);
//   - audit actions in a composite literal's Action: "..." field, in
//     constants named Action..., and as the argument of a function or method
//     of the same package whose parameter is named action.
//
// Write new codes and actions the same way, so the inventory sees them.
func scanPackage(t *testing.T, files []string, codes, actions map[string]bool) {
	t.Helper()
	fset := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	actionParams := map[string][]int{} // function or method name → indexes of parameters named action
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		parsed = append(parsed, f)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			i := 0
			for _, field := range fn.Type.Params.List {
				for _, name := range field.Names {
					if name.Name == "action" {
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

	literal := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(lit.Value)
		return s, err == nil
	}
	addCode := func(e ast.Expr) {
		if s, ok := literal(e); ok && codePattern.MatchString(s) {
			codes[s] = true
		}
	}
	addAction := func(e ast.Expr) {
		if s, ok := literal(e); ok && actionPattern.MatchString(s) {
			actions[s] = true
		}
	}

	for _, f := range parsed {
		imports := map[string]bool{}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			name := path[strings.LastIndex(path, "/")+1:]
			if imp.Name != nil {
				name = imp.Name.Name
			}
			imports[name] = true
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				mapping := isName(n.Type, "httpx", "Mapping", f.Name.Name)
				for _, elt := range n.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					switch key, _ := kv.Key.(*ast.Ident); {
					case key == nil:
					case mapping && key.Name == "Code":
						addCode(kv.Value)
					case key.Name == "Action":
						addAction(kv.Value)
					}
				}
			case *ast.CallExpr:
				if isName(n.Fun, "httpx", "NewProblem", f.Name.Name) && len(n.Args) == 3 {
					addCode(n.Args[1])
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
					if i < len(n.Args) {
						addAction(n.Args[i])
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
							addAction(vs.Values[i])
						}
					}
				}
			}
			return true
		})
	}
}

// isName reports whether e names pkg.name, or name inside package pkg itself.
func isName(e ast.Expr, pkg, name, filePkg string) bool {
	switch e := e.(type) {
	case *ast.SelectorExpr:
		x, ok := e.X.(*ast.Ident)
		return ok && x.Name == pkg && e.Sel.Name == name
	case *ast.Ident:
		return filePkg == pkg && e.Name == name
	}
	return false
}
