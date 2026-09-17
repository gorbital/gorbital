package cli

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The checks orb doctor runs in apps on gorbital.Main (ADR-0083): the module
// list, the middleware stack and the request timeout.

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

	// Directories with Go code but no func Module() aren't in the app.
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || slices.Contains(found, name) {
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
