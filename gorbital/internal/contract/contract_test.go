// Package contract_test checks that the built-in modules moved from the v0.1
// golden apps keep their contract (roadmap item 56): the OpenAPI document of
// an app built with gorbital.Main and opshttp, flagshttp and mailevents is
// compatible with the frozen v0.1.0 document for /ops/, /v1/flags and
// /v1/webhooks/resend, and their error codes, audit actions, permissions
// and roles include every one v0.1.0 had.
package contract_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/mailevents"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/modules/openapi"
)

// repo is the repository root, relative to this package.
var repo = filepath.Join("..", "..", "..")

// fixtures are the frozen v0.1.0 contracts.
var fixtures = filepath.Join(repo, "internal", "contracts", "v0.1.0")

// builtIn are the modules under test, as an app adds them.
func builtIn() []gorbital.Module {
	return []gorbital.Module{opshttp.Module(), flagshttp.Module(), mailevents.Module()}
}

// prefixes are the endpoints the modules serve.
var prefixes = []string{"/ops/", "/v1/flags", "/v1/webhooks/resend"}

// exportEnv runs the test binary as an app's main.go instead of the tests.
const exportEnv = "GORBITAL_CONTRACT_EXPORT"

func TestMain(m *testing.M) {
	if os.Getenv(exportEnv) == "1" {
		os.Args = []string{"contract", "openapi"}
		gorbital.Main(gorbital.WithName("acme-api"), gorbital.WithModules(builtIn()...))
	}
	os.Exit(m.Run())
}

// exportOpenAPI returns the document gorbital.Main's openapi command
// prints for an app with the built-in modules, built without a database.
func exportOpenAPI(t *testing.T) []byte {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), exportEnv+"=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("openapi: %v\n%s", err, stderr.Bytes())
	}
	return stdout.Bytes()
}

// signInPaths are the operations under /ops/ that sign-in serves in v0.1
// apps: they move into the library with it (Phase 5).
var signInPaths = []string{"/ops/auth/users", "/ops/service-accounts"}

func TestOpenAPICompatibleWithV010(t *testing.T) {
	current := exportOpenAPI(t)
	for _, app := range []string{"full-single", "full-multi"} {
		baseline := withoutPaths(t, read(t, filepath.Join(fixtures, "examples", app, "api", "openapi.json")), signInPaths)
		for _, prefix := range prefixes {
			found, err := openapi.CheckCompatible(baseline, current, prefix)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range found {
				t.Errorf("%s %s*: breaking change compared with v0.1.0: %s", app, prefix, f)
			}
		}
	}
	// Every v0.1.0 operation is still there, and nothing else under the
	// prefixes: the modules don't grow the contract by accident.
	baseline := withoutPaths(t, read(t, filepath.Join(fixtures, "examples", "full-single", "api", "openapi.json")), signInPaths)
	want, got := operations(t, baseline), operations(t, current)
	if !slices.Equal(want, got) {
		t.Errorf("operations under %v:\n got %v\nwant %v", prefixes, got, want)
	}
}

// TestOpenAPISameAsV010 is stricter than compatibility: each operation of
// the modules is described exactly as in v0.1.0, with the schemas it uses,
// except for the guards gorbital's router lists (x-gorbital-guards).
func TestOpenAPISameAsV010(t *testing.T) {
	type document struct {
		Paths      map[string]map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	decode := func(data []byte) document {
		var d document
		if err := json.Unmarshal(data, &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	old := decode(read(t, filepath.Join(fixtures, "examples", "full-single", "api", "openapi.json")))
	current := decode(exportOpenAPI(t))
	compared := 0
	for path, methods := range old.Paths {
		if !slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(path, p) }) ||
			slices.ContainsFunc(signInPaths, func(p string) bool { return strings.HasPrefix(path, p) }) {
			continue
		}
		for method, op := range methods {
			got := current.Paths[path][method]
			if got == nil {
				t.Errorf("%s %s: missing", method, path)
				continue
			}
			if _, ok := got["x-gorbital-guards"]; !ok {
				t.Errorf("%s %s: no x-gorbital-guards", method, path)
			}
			delete(got, "x-gorbital-guards")
			if want, have := mustJSON(t, op), mustJSON(t, got); want != have {
				t.Errorf("%s %s differs from v0.1.0:\n got %s\nwant %s", method, path, have, want)
			}
			compared++
		}
	}
	if compared < 60 {
		t.Errorf("compared %d operations, want every one of the modules", compared)
	}
	for name, want := range old.Components.Schemas {
		if got, ok := current.Components.Schemas[name]; ok && mustJSON(t, got) != mustJSON(t, want) {
			t.Errorf("schema %s differs from v0.1.0", name)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
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
		if !slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(path, p) }) {
			continue
		}
		for method, op := range methods {
			out = append(out, strings.ToUpper(method)+" "+path+" "+op.OperationID)
		}
	}
	slices.Sort(out)
	return out
}

// withoutPaths removes the paths starting with any of prefixes from an
// OpenAPI document.
func withoutPaths(t *testing.T, doc []byte, prefixes []string) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatal(err)
	}
	paths, _ := root["paths"].(map[string]any)
	for path := range paths {
		if slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(path, p) }) {
			delete(paths, path)
		}
	}
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// surface is the v0.1.0 api/surface.json of a golden app.
type surface struct {
	ErrorCodes   []string            `json:"error_codes"`
	AuditActions []string            `json:"audit_actions"`
	Permissions  map[string][]string `json:"permissions"`
	Roles        map[string][]string `json:"roles"`
}

func frozenSurface(t *testing.T) surface {
	t.Helper()
	var s surface
	if err := json.Unmarshal(read(t, filepath.Join(fixtures, "examples", "full-single", "api", "surface.json")), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// golden is where the v0.1 modules and their wiring still live, unconverted
// until Phase 9.
var golden = filepath.Join(repo, "examples", "full-single", "internal")

var codePattern = regexp.MustCompile(`Code: "([a-z_]+)"`)

func TestErrorCodesKeepV010(t *testing.T) {
	var want []string
	for _, file := range []string{"module_ops.go", "module_flags.go", "module_mailevents.go"} {
		for _, m := range codePattern.FindAllStringSubmatch(string(read(t, filepath.Join(golden, "app", file))), -1) {
			want = append(want, m[1])
		}
	}
	if len(want) < 40 {
		t.Fatalf("found %d error codes in the golden app's module files, want every mapping", len(want))
	}
	frozen := frozenSurface(t).ErrorCodes
	var got []string
	for _, m := range builtIn() {
		for _, mapping := range m.Errors {
			got = append(got, mapping.Code)
		}
	}
	for _, code := range want {
		if !slices.Contains(frozen, code) {
			t.Errorf("error code %q of the golden app isn't in the v0.1.0 surface; is the fixture right?", code)
		}
		if !slices.Contains(got, code) {
			t.Errorf("error code %q of v0.1.0 isn't mapped by the built-in modules", code)
		}
	}
}

var actionPattern = regexp.MustCompile(`(?:Action:\s*|Action[A-Za-z]*\s*=\s*)"([a-z_]+(?:\.[a-z_]+)+)"`)

// actions returns the audit actions written in the Go files under dir.
func actions(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		data, err := os.ReadFile(path) //nolint:gosec // the repository's own files
		if err != nil {
			return err
		}
		for _, m := range actionPattern.FindAllStringSubmatch(string(data), -1) {
			out = append(out, m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func TestAuditActionsKeepV010(t *testing.T) {
	var want []string
	for _, module := range []string{"ops", "flags", "mailevents"} {
		want = append(want, actions(t, filepath.Join(golden, "modules", module))...)
	}
	if len(want) < 10 {
		t.Fatalf("found %d audit actions in the golden modules, want all of them", len(want))
	}
	frozen := frozenSurface(t).AuditActions
	var got []string
	for _, pkg := range []string{"opshttp", "flagshttp", "mailevents"} {
		got = append(got, actions(t, filepath.Join("..", "..", pkg))...)
	}
	for _, action := range want {
		if !slices.Contains(frozen, action) {
			t.Errorf("audit action %q of the golden modules isn't in the v0.1.0 surface; is the fixture right?", action)
		}
		if !slices.Contains(got, action) {
			t.Errorf("audit action %q of v0.1.0 isn't recorded by the built-in modules", action)
		}
	}
}

// signIns are the v0.1.0 platform permissions sign-in (authhttp) declares:
// not the operations API's, though their names start with ops. The
// operations API checks ops.auth.read and ops.auth.write too.
var signIns = []string{"ops.auth.read", "ops.auth.write", "ops.service_accounts.read", "ops.service_accounts.write"}

func TestPermissionsAndRolesKeepV010(t *testing.T) {
	frozen := frozenSurface(t)
	var want []string
	for _, p := range frozen.Permissions["platform"] {
		if (strings.HasPrefix(p, "ops.") || strings.HasPrefix(p, "flags.")) && !slices.Contains(signIns, p) {
			want = append(want, p)
		}
	}
	if len(want) < 19 {
		t.Fatalf("found %d ops and flags permissions in the v0.1.0 surface, want all of them", len(want))
	}
	modules := builtIn()
	var got []string
	for _, m := range modules {
		for _, p := range m.Permissions {
			got = append(got, p.Name)
		}
	}
	for _, p := range want {
		if !slices.Contains(got, p) {
			t.Errorf("permission %q of v0.1.0 isn't declared by the built-in modules", p)
		}
	}

	// The roles keep their v0.1.0 permissions, but those of sign-in.
	oldRoles := v010Roles(t)
	for role, perms := range oldRoles {
		grants := gorbital.Grants(role, modules...)
		for _, p := range perms {
			if !slices.Contains(signIns, p) && (strings.HasPrefix(p, "ops.") || p == flagshttp.PermRead) && !slices.Contains(grants, p) {
				t.Errorf("role %s doesn't grant %s, as it did in v0.1.0", role, p)
			}
		}
		if !slices.Contains(frozen.Roles["platform"], role) {
			t.Errorf("role %s isn't in the v0.1.0 surface", role)
		}
	}
}

// v010Roles returns the ops roles and the permissions the golden app's
// permissions.go grants them, reading its declarations.
func v010Roles(t *testing.T) map[string][]string {
	t.Helper()
	src := string(read(t, filepath.Join(golden, "app", "permissions.go")))
	constants := map[string]string{} // qualified as permissions.go writes them, such as opsdomain.PermJobsRead
	for qualifier, file := range map[string]string{
		"opsdomain":    filepath.Join(golden, "modules", "ops", "domain", "permissions.go"),
		"flagsusecase": filepath.Join(golden, "modules", "flags", "usecase", "service.go"),
	} {
		for _, m := range regexp.MustCompile(`(Perm[A-Za-z]+)\s*=\s*"([a-z_.]+)"`).FindAllStringSubmatch(string(read(t, file)), -1) {
			constants[qualifier+"."+m[1]] = m[2]
		}
	}
	names := func(list string) []string {
		var out []string
		for _, m := range regexp.MustCompile(`[a-z]+\.Perm[A-Za-z]+`).FindAllString(list, -1) {
			if v, ok := constants[m]; ok {
				out = append(out, v)
			}
		}
		return out
	}
	viewer := regexp.MustCompile(`(?s)c\.Role\(roleOpsViewer, "[^"]*",(.*?)\)\n`).FindStringSubmatch(src)
	if viewer == nil {
		t.Fatal("no ops_viewer role in the golden app's permissions.go")
	}
	all := []string{}
	for _, v := range constants {
		if strings.HasPrefix(v, "ops.") {
			all = append(all, v)
		}
	}
	return map[string][]string{"platform_admin": all, "ops_viewer": names(viewer[1]), "user": {flagshttp.PermRead}}
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // the repository's own files
	if err != nil {
		t.Fatal(err)
	}
	return data
}
