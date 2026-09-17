package integration_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/mailevents"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/openapi"
)

// fixtures are the frozen v0.1.0 contracts.
var fixtures = filepath.Join(repo, "internal", "contracts", "v0.1.0")

// prefixes are the endpoints the built-in modules serve.
var prefixes = []string{"/v1/auth/", "/ops/", "/v1/flags", "/v1/webhooks/resend"}

// exportEnv runs the test binary as an app's main.go instead of the tests.
const exportEnv = "GORBITAL_INTEGRATION_EXPORT"

func TestMain(m *testing.M) {
	switch os.Getenv(exportEnv) {
	case "1":
		os.Args = []string{"integration", "openapi"}
		gorbital.Main(options(authhttp.New())...)
	case "multi":
		os.Args = []string{"integration", "openapi"}
		gorbital.Main(multiOptions(authhttp.New())...)
	}
	os.Exit(m.Run())
}

// exportOpenAPI returns the document gorbital.Main's openapi command prints
// for the app with every built-in module, built without a database.
func exportOpenAPI(t *testing.T) []byte {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), exportEnv+"=1", "APP_ENV=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("openapi: %v\n%s", err, stderr.Bytes())
	}
	return stdout.Bytes()
}

// TestOpenAPIKeepsV010 checks the document of the app with every built-in
// module against both golden apps' frozen v0.1.0 documents: compatible under
// each prefix, every v0.1.0 operation present with its operation ID, and
// each described as in v0.1.0 but for the guards gorbital's router lists.
func TestOpenAPIKeepsV010(t *testing.T) {
	current := exportOpenAPI(t)
	for _, golden := range []string{"full-single", "full-multi"} {
		baseline := []byte(read(t, filepath.Join(fixtures, "examples", golden, "api", "openapi.json")))
		for _, prefix := range prefixes {
			found, err := openapi.CheckCompatible(baseline, current, prefix)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range found {
				t.Errorf("%s %s*: breaking change compared with v0.1.0: %s", golden, prefix, f)
			}
		}
		got := operations(t, current)
		for _, op := range operations(t, baseline) {
			if !slices.Contains(got, op) {
				t.Errorf("%s: operation %q of v0.1.0 is missing", golden, op)
			}
		}
	}

	type document struct {
		Paths map[string]map[string]map[string]any `json:"paths"`
	}
	var old, now document
	if err := json.Unmarshal([]byte(read(t, filepath.Join(fixtures, "examples", "full-single", "api", "openapi.json"))), &old); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(current, &now); err != nil {
		t.Fatal(err)
	}
	compared := 0
	for path, methods := range old.Paths {
		if !underPrefixes(path) {
			continue
		}
		for method, op := range methods {
			got := now.Paths[path][method]
			if got == nil {
				continue // reported above
			}
			if _, ok := got["x-gorbital-guards"]; !ok {
				t.Errorf("%s %s: no x-gorbital-guards", method, path)
			}
			delete(got, "x-gorbital-guards")
			if want, have := marshal(t, op), marshal(t, got); want != have {
				t.Errorf("%s %s differs from v0.1.0:\n got %s\nwant %s", method, path, have, want)
			}
			compared++
		}
	}
	if compared < 125 {
		t.Errorf("compared %d operations, want every one of sign-in, /ops, flags and mail events", compared)
	}
}

func underPrefixes(path string) bool {
	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(path, p) })
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// operations lists "METHOD path operationId" under prefixes, sorted.
func operations(t *testing.T, doc []byte) []string {
	t.Helper()
	var root struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatal(err)
	}
	var out []string
	for path, methods := range root.Paths {
		if !underPrefixes(path) {
			continue
		}
		for method, op := range methods {
			out = append(out, strings.ToUpper(method)+" "+path+" "+op.OperationID)
		}
	}
	slices.Sort(out)
	return out
}

// surface is a golden app's v0.1.0 api/surface.json.
type surface struct {
	ErrorCodes   []string            `json:"error_codes"`
	AuditActions []string            `json:"audit_actions"`
	Permissions  map[string][]string `json:"permissions"`
	Roles        map[string][]string `json:"roles"`
	Settings     []string            `json:"settings"`
	Jobs         []string            `json:"jobs"`
}

// appOwned are the names of full-single's v0.1.0 surface that its own
// example modules declare (internal/modules/ping and projects, and the
// example setting, flag and job of internal/app), not the built-in modules.
var appOwned = struct {
	settings, jobs, permissionPrefixes []string
	moduleDirs                         []string
}{
	settings:           []string{"example.ping_message"},
	jobs:               []string{"heartbeat"},
	permissionPrefixes: []string{"projects."},
	moduleDirs:         []string{"modules/ping", "modules/projects", "app/module_ping.go", "app/module_projects.go"},
}

// TestNamesKeepV010 checks that an app with every built-in module has every
// public name of full-single's v0.1.0 surface but its example modules':
// error codes and audit actions in the source of the packages it links,
// platform permissions and roles, runtime settings and jobs.
func TestNamesKeepV010(t *testing.T) {
	var frozen surface
	if err := json.Unmarshal([]byte(read(t, filepath.Join(fixtures, "examples", "full-single", "api", "surface.json"))), &frozen); err != nil {
		t.Fatal(err)
	}

	// Error codes and audit actions, scanned as the golden app's
	// TestPublicSurface scans them, without the example modules'. The
	// built-in modules' error mappings are read from the modules: their
	// []httpx.Mapping literals elide the element type the scan looks for.
	codes, actions := map[string]bool{}, map[string]bool{}
	for status := 400; status <= 599; status++ {
		codes[httpx.DefaultCode(status)] = true
	}
	for _, m := range []gorbital.Module{authhttp.New().Module(), opshttp.Module(), flagshttp.Module(), mailevents.Module()} {
		for _, mapping := range m.Errors {
			codes[mapping.Code] = true
		}
	}
	for _, pkg := range linkedPackages(t) {
		scanPackage(t, pkg, codes, actions)
	}
	ownCodes, ownActions := map[string]bool{}, map[string]bool{}
	for _, dir := range appOwned.moduleDirs {
		scanPackage(t, goFiles(t, filepath.Join(golden, dir)), ownCodes, ownActions)
	}
	missing := func(kind string, want []string, got func(string) bool, own func(string) bool) {
		t.Helper()
		n := 0
		for _, name := range want {
			if own(name) {
				continue
			}
			n++
			if !got(name) {
				t.Errorf("%s %q of v0.1.0 isn't in an app with the built-in modules", kind, name)
			}
		}
		if n == 0 {
			t.Errorf("no %s of v0.1.0 compared", kind)
		}
	}
	missing("error code", frozen.ErrorCodes, func(c string) bool { return codes[c] }, func(c string) bool { return ownCodes[c] && !codes[c] })
	missing("audit action", frozen.AuditActions, func(a string) bool { return actions[a] }, func(a string) bool { return ownActions[a] && !actions[a] })

	// Permissions, roles, settings and jobs of the running app.
	a := newApp(t)
	ops := a.signUpOperator(t, "names@example.com")
	roles := map[string][]string{}
	for _, m := range regexp.MustCompile(`(?m)^([a-z_]+)\n  .*\n  permissions: (.*)$`).FindAllStringSubmatch(a.command(t, "roles"), -1) {
		roles[m[1]] = strings.Split(m[2], ", ")
	}
	var permissions []string
	for _, perms := range roles {
		permissions = append(permissions, perms...)
	}
	missing("role", frozen.Roles["platform"], func(r string) bool { _, ok := roles[r]; return ok }, func(string) bool { return false })
	missing("platform permission", frozen.Permissions["platform"], func(p string) bool { return slices.Contains(permissions, p) }, func(p string) bool {
		return slices.ContainsFunc(appOwned.permissionPrefixes, func(prefix string) bool { return strings.HasPrefix(p, prefix) })
	})

	var settings struct {
		Settings []struct {
			Key string `json:"key"`
		} `json:"settings"`
	}
	res := ops.client.Get("/ops/settings")
	res.AssertStatus(t, http.StatusOK)
	res.JSON(t, &settings)
	var keys []string
	for _, s := range settings.Settings {
		keys = append(keys, s.Key)
	}
	missing("setting", frozen.Settings, func(k string) bool { return slices.Contains(keys, k) }, func(k string) bool { return slices.Contains(appOwned.settings, k) })

	var definitions struct {
		Definitions []struct {
			Name string `json:"name"`
		} `json:"definitions"`
	}
	res = ops.client.Get("/ops/jobs/definitions")
	res.AssertStatus(t, http.StatusOK)
	res.JSON(t, &definitions)
	names := []string{jobs.MailKind} // the mail worker's kind, as v0.1's surface lists it
	for _, d := range definitions.Definitions {
		names = append(names, d.Name)
	}
	missing("job", frozen.Jobs, func(j string) bool { return slices.Contains(names, j) }, func(j string) bool { return slices.Contains(appOwned.jobs, j) })
}

// linkedPackages returns the Go files of every gorbital.dev package this
// test links, by package.
func linkedPackages(t *testing.T) [][]string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", "-test", "-f", `{{if .Module}}{{.Module.Path}}|{{.Dir}}|{{join .GoFiles ","}}{{end}}`, ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs [][]string
	seen := map[string]bool{}
	for line := range strings.Lines(string(out)) {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
		if len(parts) != 3 || parts[2] == "" || seen[parts[1]] {
			continue
		}
		if parts[0] != "gorbital.dev" && !strings.HasPrefix(parts[0], "gorbital.dev/") {
			continue
		}
		seen[parts[1]] = true
		var files []string
		for f := range strings.SplitSeq(parts[2], ",") {
			files = append(files, filepath.Join(parts[1], f))
		}
		pkgs = append(pkgs, files)
	}
	if len(pkgs) < 20 {
		t.Fatalf("go list found %d gorbital.dev packages", len(pkgs))
	}
	return pkgs
}

// goFiles returns the non-test Go files under path, or path itself when it
// is a file. The golden modules' layers are separate packages, which
// scanPackage parses together without harm.
func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

var (
	codePattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	actionPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

// scanPackage adds the error codes and audit actions written in a package's
// files, as a v0.1 app's TestPublicSurface finds them: codes in
// httpx.Mapping{Code: "..."} and httpx.NewProblem(status, "...", detail);
// actions in a composite literal's Action field, in constants named
// Action..., and as the argument of a function of the package whose
// parameter is named action.
func scanPackage(t *testing.T, files []string, codes, actions map[string]bool) {
	t.Helper()
	fset := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	actionParams := map[string][]int{}
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
	add := func(set map[string]bool, pattern *regexp.Regexp, e ast.Expr) {
		if s, ok := literal(e); ok && pattern.MatchString(s) {
			set[s] = true
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
						add(codes, codePattern, kv.Value)
					case key.Name == "Action":
						add(actions, actionPattern, kv.Value)
					}
				}
			case *ast.CallExpr:
				if isName(n.Fun, "httpx", "NewProblem", f.Name.Name) && len(n.Args) == 3 {
					add(codes, codePattern, n.Args[1])
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
						add(actions, actionPattern, n.Args[i])
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
							add(actions, actionPattern, vs.Values[i])
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
