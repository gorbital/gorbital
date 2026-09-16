//go:build ignore

// This file isn't built into refdocs: refdocs adds it to a golden app's
// internal/app package for one go test -overlay run (removing the build
// constraint above), so it reads the app's real declarations while the app
// carries no documentation code. It uses the app's unexported
// permissionCatalogs, settings store and jobs manager, and the test database.

package app

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gorbital.dev/config"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres/pgtest"
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
	d := refDump{ServiceName: ServiceName}

	for name, c := range permissionCatalogs() {
		rc := refCatalog{Name: name}
		for _, p := range c.AllPermissions() {
			rc.Permissions = append(rc.Permissions, refPermission{p.Name, p.Description})
		}
		for _, r := range c.Roles() {
			rc.Roles = append(rc.Roles, refRole{r.Name, r.Description, r.Permissions, c.RequiresMFA(r.Name)})
		}
		d.Catalogs = append(d.Catalogs, rc)
	}

	url := pgtest.NewDatabase(t)
	cfg, err := LoadConfig(config.Source{Getenv: func(key string) string {
		switch key {
		case "APP_ENV":
			return "development"
		case "DATABASE_URL":
			return url
		}
		return ""
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, cfg, io.Discard); err != nil {
		t.Fatal(err)
	}
	a, err := New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(ctx)

	for _, v := range a.settings.List() {
		d.Settings = append(d.Settings, refSetting{v.Key, string(v.Kind), v.Group, v.Description, v.Default, v.ReasonRequired, v.RestartRequired, v.Constraints})
	}
	defs, err := a.jobsManager.Definitions(ctx)
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

// scanReference finds error codes and audit actions the way
// TestPublicSurface does, keeping each one's status, detail, metadata keys
// and where it is written.
func scanReference(t *testing.T, d *refDump) {
	t.Helper()
	statuses := httpStatuses(t)
	module := goOutput(t, "list", "-m")
	for _, line := range strings.Split(goOutput(t, "list", "-deps", "-f", `{{if .Module}}{{.Module.Path}}|{{.Module.Dir}}|{{.ImportPath}}|{{.Dir}}|{{join .GoFiles ","}}{{end}}`, "./..."), "\n") {
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
							if lit, ok := valueAt(vs, i).(*ast.BasicLit); ok && strings.HasPrefix(id.Name, "Action") {
								actionConsts[id.Name], _ = strconv.Unquote(lit.Value)
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
			scanFile(f, location, statuses, actionParams, actionConsts, d)
		}
	}
}

func scanFile(f *ast.File, location string, statuses map[string]int, actionParams map[string][]int, actionConsts map[string]string, d *refDump) {
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
	isAction := func(s string) bool { return actionPattern.MatchString(s) }

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
		ast.Inspect(decl, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
					if id, ok := n.Lhs[0].(*ast.Ident); ok {
						assigned[n.Rhs[0]] = id.Name
					}
				}
			case *ast.CompositeLit:
				mapping := isName(n.Type, "httpx", "Mapping", f.Name.Name)
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
						code, _ = literal(kv.Value)
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
				if code != "" && codePattern.MatchString(code) {
					d.Codes = append(d.Codes, refCode{Code: code, Status: st, Detail: detail, Location: location})
				}
				if isAction(action) {
					d.Actions = append(d.Actions, refAction{Action: action, Metadata: append(meta, laterKeys(body, assigned[n], n.Pos())...), Location: location})
				}
			case *ast.CallExpr:
				if isName(n.Fun, "httpx", "NewProblem", f.Name.Name) && len(n.Args) == 3 {
					if code, ok := literal(n.Args[1]); ok && codePattern.MatchString(code) {
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
