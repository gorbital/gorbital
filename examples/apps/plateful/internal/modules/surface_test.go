package modules_test

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

	"gorbital.dev/gorbital"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/settings"

	"example.com/plateful/internal/modules"
)

var updateSurface = flag.Bool("update", false, "record the app's public names in api/surface.json")

// surfacePath is the recorded public surface, relative to this package.
var surfacePath = filepath.Join("..", "..", "api", "surface.json")

// surface is the app's own public surface besides its HTTP endpoints
// (ADR-0015, ADR-0054): the names the app's modules declare, which clients,
// operators, dashboards and stored data depend on. Every list is sorted.
//
// Names of gorbital's built-in modules (sign-in, /ops, flags, email events,
// organisations) and of the library packages the app links aren't recorded
// here: the library keeps them, and a library upgrade that adds names must
// not fail the app's tests (ADR-0083, Phase 9 notes).
type surface struct {
	// ErrorCodes are the problem+json codes the app's code returns: its
	// modules' error mappings and httpx.NewProblem calls.
	ErrorCodes []string `json:"error_codes"`
	// AuditActions are the actions of the audit events the app records.
	AuditActions []string `json:"audit_actions"`
	// Permissions are the modules' permissions, and Roles the roles they
	// grant them to: "platform" roles, and "org" roles inside organisations.
	Permissions map[string][]string `json:"permissions"`
	Roles       map[string][]string `json:"roles"`
	// Settings are the modules' runtime setting keys.
	Settings []string `json:"settings"`
	// Jobs are the modules' job definition names.
	Jobs []string `json:"jobs"`
	// Flags are the modules' feature flag keys (ADR-0057).
	Flags []string `json:"flags"`
}

// TestPublicSurface fails when a public name recorded in api/surface.json
// disappears (a breaking change for clients and operators) or when one of
// the app's new names isn't recorded yet. It reads modules.All() and the
// app's source, without a database. After adding error codes, audit
// actions, permissions, settings, jobs or feature flags, record them with
//
//	go test ./internal/modules -run TestPublicSurface -update
//
// and commit api/surface.json; review removed lines like any breaking
// change. A module main.go adds outside modules.All belongs in
// surfaceModules too.
func TestPublicSurface(t *testing.T) {
	got := currentSurface(t, surfaceModules())
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
		t.Fatalf("read api/surface.json: %v; record it with: go test ./internal/modules -run TestPublicSurface -update", err)
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
		{"feature flag", want.Flags, got.Flags},
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
		t.Error("api/surface.json isn't formatted as recorded; run: go test ./internal/modules -run TestPublicSurface -update")
	}
}

// surfaceModules are the app's modules whose names are public API.
func surfaceModules() []gorbital.Module { return modules.All() }

func compareNames(t *testing.T, kind string, want, got []string) {
	t.Helper()
	for _, name := range want {
		if !slices.Contains(got, name) {
			t.Errorf("%s %q is recorded in api/surface.json but no longer exists; it is public API (ADR-0015): restore it, or remove it deliberately with -update and call it out as a breaking change", kind, name)
		}
	}
	for _, name := range got {
		if !slices.Contains(want, name) {
			t.Errorf("new %s %q isn't recorded; run: go test ./internal/modules -run TestPublicSurface -update", kind, name)
		}
	}
}

func mergeKeys(a, b map[string][]string) map[string][]string {
	all := map[string][]string{}
	for k := range a {
		all[k] = nil
	}
	for k := range b {
		all[k] = nil
	}
	return all
}

// currentSurface reads the names the modules declare and the app's source
// writes.
func currentSurface(t *testing.T, mods []gorbital.Module) surface {
	t.Helper()
	platform, org := auth.NewCatalog(), auth.NewCatalog()
	settingsReg, flagsReg := settings.NewRegistry(), flags.NewRegistry()
	if err := gorbital.Declare(gorbital.Declarations{Permissions: platform, OrgPermissions: org, Settings: settingsReg, Flags: flagsReg}, mods...); err != nil {
		t.Fatal(err)
	}
	s := surface{Permissions: map[string][]string{}, Roles: map[string][]string{}}
	for name, c := range map[string]*auth.Catalog{"platform": platform, "org": org} {
		for _, p := range c.AllPermissions() {
			s.Permissions[name] = append(s.Permissions[name], p.Name)
		}
		slices.Sort(s.Permissions[name])
	}
	roles := map[string]map[string]bool{"platform": {}, "org": {}}
	codes, actions := map[string]bool{}, map[string]bool{}
	defs := jobs.NewDefinitions()
	for _, m := range mods {
		for _, p := range m.Permissions {
			for _, r := range p.Roles {
				roles["platform"][r] = true
			}
			for _, r := range p.OrgRoles {
				roles["org"][r] = true
			}
		}
		for _, mapping := range m.Errors {
			codes[mapping.Code] = true
		}
		if m.Jobs != nil {
			m.Jobs(defs, gorbital.Deps{})
		}
	}
	for name, set := range roles {
		if len(set) > 0 {
			s.Roles[name] = slices.Sorted(maps.Keys(set))
		}
	}
	for name := range s.Permissions {
		if len(s.Permissions[name]) == 0 {
			delete(s.Permissions, name)
		}
	}
	s.Settings = slices.Sorted(slices.Values(settingsReg.Keys()))
	s.Flags = slices.Sorted(slices.Values(flagsReg.Keys()))
	s.Jobs = slices.Sorted(slices.Values(defs.Names()))

	for _, pkg := range appPackages(t) {
		scanPackage(t, pkg, codes, actions)
	}
	s.ErrorCodes = slices.Sorted(maps.Keys(codes))
	s.AuditActions = slices.Sorted(maps.Keys(actions))
	for _, list := range []*[]string{&s.ErrorCodes, &s.AuditActions, &s.Settings, &s.Jobs, &s.Flags} {
		if *list == nil {
			*list = []string{}
		}
	}
	return s
}

// appPackages returns the Go files of every package of the app's own Go
// module, by package.
func appPackages(t *testing.T) [][]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-f", `{{.Dir}}|{{join .GoFiles ","}}`, "./...")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs [][]string
	for line := range strings.Lines(string(out)) {
		dir, names, _ := strings.Cut(strings.TrimSpace(line), "|")
		if names == "" {
			continue
		}
		var files []string
		for _, f := range strings.Split(names, ",") {
			files = append(files, filepath.Join(dir, f))
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
