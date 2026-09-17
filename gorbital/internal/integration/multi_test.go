package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/mailevents"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/gorbital/orgshttp"
	"gorbital.dev/httpx"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/openapi"
)

// The tests in this file check a multi-tenant app, full-multi's v0.1 wiring
// replaced by the built-in modules with organisations (orgshttp), against
// full-multi's frozen v0.1.0 contracts (roadmap item 73).

// goldenMulti is the multi-tenant golden app.
var goldenMulti = filepath.Join(repo, "examples", "full-multi", "internal")

// multiPrefixes are the endpoints of the built-in modules of a multi-tenant
// app: prefixes and organisations'.
var multiPrefixes = append(slices.Clone(prefixes), "/v1/orgs", "/v1/invitations")

// multiAppOwned reports paths of full-multi's example module under the
// organisations' prefix.
func multiAppOwned(path string) bool { return strings.HasPrefix(path, "/v1/orgs/{orgId}/projects") }

// multiOptions are the options of a multi-tenant app with every built-in
// module, as main.go adds them.
func multiOptions(auth *authhttp.Authenticator, extra ...gorbital.Module) []gorbital.Option {
	return append(options(auth), gorbital.WithModules(append([]gorbital.Module{orgshttp.Module(auth)}, extra...)...))
}

// exportMultiOpenAPI returns the document gorbital.Main's openapi command
// prints for the multi-tenant app.
func exportMultiOpenAPI(t *testing.T) []byte {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), exportEnv+"=multi", "APP_ENV=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("openapi: %v\n%s", err, stderr.Bytes())
	}
	return stdout.Bytes()
}

// TestOpenAPIKeepsFullMultiV010 checks the document of the multi-tenant app
// against full-multi's frozen v0.1.0 document: compatible under every
// prefix of the built-in modules, organisations included; every v0.1.0
// operation of theirs present with its operation ID; each described as in
// v0.1.0 but for x-gorbital-guards, with every schema it references
// identical.
func TestOpenAPIKeepsFullMultiV010(t *testing.T) {
	current := exportMultiOpenAPI(t)
	baseline := []byte(read(t, filepath.Join(fixtures, "examples", "full-multi", "api", "openapi.json")))
	for _, prefix := range multiPrefixes {
		found, err := openapi.CheckCompatible(baseline, current, prefix)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range found {
			if strings.Contains(f.Operation, "/v1/orgs/{orgId}/projects") {
				continue // full-multi's example module
			}
			t.Errorf("full-multi %s*: breaking change compared with v0.1.0: %s", prefix, f)
		}
	}

	type document struct {
		Paths      map[string]map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]any `json:"schemas"`
		} `json:"components"`
	}
	var old, now document
	if err := json.Unmarshal(baseline, &old); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(current, &now); err != nil {
		t.Fatal(err)
	}
	// Huma names an operation's anonymous body schema after its input type,
	// with a number when another module registered the name first: in
	// full-multi, the example projects module's CreateInputBody made
	// organisations' CreateInputBody1. Such names depend on the app's own
	// modules, in v0.1 as now; a numbered name compares by content.
	renamed := map[string]string{}
	for name, schema := range old.Components.Schemas {
		if !numberedBody.MatchString(name) {
			continue
		}
		base := strings.TrimRight(name, "0123456789")
		if marshal(t, now.Components.Schemas[base]) == marshal(t, schema) && marshal(t, old.Components.Schemas[base]) != marshal(t, schema) {
			renamed["#/components/schemas/"+name+`"`] = "#/components/schemas/" + base + `"`
		}
	}
	compared, orgOps := 0, 0
	refs := map[string]bool{}
	for path, methods := range old.Paths {
		if !slices.ContainsFunc(multiPrefixes, func(p string) bool { return strings.HasPrefix(path, p) }) || multiAppOwned(path) {
			continue
		}
		for method, op := range methods {
			got := now.Paths[path][method]
			if got == nil {
				t.Errorf("%s %s (%v) of v0.1.0 is missing", method, path, op["operationId"])
				continue
			}
			if _, ok := got["x-gorbital-guards"]; !ok {
				t.Errorf("%s %s: no x-gorbital-guards", method, path)
			}
			delete(got, "x-gorbital-guards")
			want, have := marshal(t, op), marshal(t, got)
			for _, m := range schemaRef.FindAllStringSubmatch(want, -1) {
				refs[m[1]] = true
			}
			for from, to := range renamed {
				want = strings.ReplaceAll(want, from, to)
			}
			if want != have {
				t.Errorf("%s %s differs from v0.1.0:\n got %s\nwant %s", method, path, have, want)
			}
			compared++
			if strings.HasPrefix(path, "/v1/orgs") || strings.HasPrefix(path, "/v1/invitations") {
				orgOps++
			}
		}
	}
	if orgOps < 29 || compared < 156 {
		t.Errorf("compared %d operations, %d of organisations; want every one of full-multi's built-in modules", compared, orgOps)
	}
	// Schemas referenced from schemas are compared too.
	for changed := true; changed; {
		changed = false
		for name := range refs {
			for _, m := range schemaRef.FindAllStringSubmatch(marshal(t, old.Components.Schemas[name]), -1) {
				if !refs[m[1]] {
					refs[m[1]], changed = true, true
				}
			}
		}
	}
	for name := range refs {
		current := name
		if to, ok := renamed["#/components/schemas/"+name+`"`]; ok {
			current = strings.TrimSuffix(strings.TrimPrefix(to, "#/components/schemas/"), `"`)
		}
		if want, have := marshal(t, old.Components.Schemas[name]), marshal(t, now.Components.Schemas[current]); want != have {
			t.Errorf("schema %s differs from v0.1.0:\n got %s\nwant %s", name, have, want)
		}
	}
}

var (
	schemaRef    = regexp.MustCompile(`#/components/schemas/([A-Za-z0-9_]+)`)
	numberedBody = regexp.MustCompile(`^[A-Za-z]+InputBody[0-9]+$`)
)

// orgCatalog captures the organisation catalog the app built.
type orgCatalog struct{ catalog *authlib.Catalog }

func (c *orgCatalog) module() gorbital.Module {
	return gorbital.Module{Name: "org_catalog_probe", Platform: func(p *gorbital.Platform) error {
		c.catalog = p.OrgPermissions
		return nil
	}}
}

// TestNamesKeepFullMultiV010 checks that a multi-tenant app with every
// built-in module has every public name of full-multi's v0.1.0 surface but
// its example modules': error codes and audit actions, platform and
// organisation permissions and roles with their grants, runtime settings,
// jobs, and the rows of /ops/retention in v0.1's order.
func TestNamesKeepFullMultiV010(t *testing.T) {
	var frozen surface
	if err := json.Unmarshal([]byte(read(t, filepath.Join(fixtures, "examples", "full-multi", "api", "surface.json"))), &frozen); err != nil {
		t.Fatal(err)
	}
	auth := authhttp.New()
	codes, actions := map[string]bool{}, map[string]bool{}
	for status := 400; status <= 599; status++ {
		codes[httpx.DefaultCode(status)] = true
	}
	for _, m := range []gorbital.Module{auth.Module(), opshttp.Module(), flagshttp.Module(), mailevents.Module(), orgshttp.Module(auth)} {
		for _, mapping := range m.Errors {
			codes[mapping.Code] = true
		}
	}
	for _, pkg := range linkedPackages(t) {
		scanPackage(t, pkg, codes, actions)
	}
	ownCodes, ownActions := map[string]bool{}, map[string]bool{}
	for _, dir := range appOwned.moduleDirs {
		scanPackage(t, goFiles(t, filepath.Join(goldenMulti, dir)), ownCodes, ownActions)
	}
	check := func(kind string, want []string, got, own func(string) bool) {
		t.Helper()
		n := 0
		for _, name := range want {
			if own(name) {
				continue
			}
			n++
			if !got(name) {
				t.Errorf("%s %q of full-multi v0.1.0 isn't in a multi-tenant app with the built-in modules", kind, name)
			}
		}
		if n == 0 {
			t.Errorf("no %s of v0.1.0 compared", kind)
		}
	}
	check("error code", frozen.ErrorCodes, func(c string) bool { return codes[c] }, func(c string) bool { return ownCodes[c] && !codes[c] })
	check("audit action", frozen.AuditActions, func(a string) bool { return actions[a] }, func(a string) bool { return ownActions[a] && !actions[a] })

	probe := &orgCatalog{}
	g := gorbitaltest.NewWithEnv(t, map[string]string{"AUTH_ENCRYPTION_KEYS": encryptionKeys}, multiOptions(auth, probe.module())...)
	a := &app{App: g, auth: auth}
	ops := a.signUpOperator(t, "names@example.com")

	// Platform roles and permissions.
	roles := map[string][]string{}
	for _, m := range regexp.MustCompile(`(?m)^([a-z_]+)\n  .*\n  permissions: (.*)$`).FindAllStringSubmatch(a.command(t, "roles"), -1) {
		roles[m[1]] = strings.Split(m[2], ", ")
	}
	var permissions []string
	for _, perms := range roles {
		permissions = append(permissions, perms...)
	}
	projects := func(p string) bool { return strings.HasPrefix(p, "projects.") }
	check("platform role", frozen.Roles["platform"], func(r string) bool { _, ok := roles[r]; return ok }, func(string) bool { return false })
	check("platform permission", frozen.Permissions["platform"], func(p string) bool { return slices.Contains(permissions, p) }, projects)
	if !slices.Contains(roles["user"], "orgs.org.create") || !slices.Contains(roles["user"], "orgs.org.list") {
		t.Errorf("the user role holds %v, want orgs.org.create and orgs.org.list as in v0.1", roles["user"])
	}

	// Organisation roles, permissions and grants, as full-multi's
	// declareOrgPermissions declares them without its projects.
	c := probe.catalog
	if c == nil {
		t.Fatal("no organisation catalog")
	}
	var orgRoles []string
	for _, r := range c.Roles() {
		orgRoles = append(orgRoles, r.Name)
	}
	if want := []string{"owner", "admin", "member"}; !slices.Equal(orgRoles, want) {
		t.Errorf("organisation roles = %v, want v0.1's %v in order", orgRoles, want)
	}
	check("organisation role", frozen.Roles["org"], func(r string) bool { return c.HasRole(r) }, func(string) bool { return false })
	member := []string{"orgs.members.read", "orgs.org.read", "orgs.settings.read"}
	admin := append([]string{"orgs.members.manage", "orgs.org.update", "orgs.service_accounts.manage", "orgs.settings.write"}, member...)
	owner := append([]string{"orgs.org.delete"}, admin...)
	for role, want := range map[string][]string{"owner": owner, "admin": admin, "member": member} {
		got := slices.Clone(c.Permissions(role))
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("organisation role %s holds %v, want v0.1's %v", role, got, want)
		}
	}
	for _, p := range frozen.Permissions["org"] {
		if projects(p) {
			continue
		}
		if !slices.ContainsFunc(orgRoles, func(r string) bool { return slices.Contains(c.Permissions(r), p) }) {
			t.Errorf("organisation permission %q of v0.1.0 isn't granted by any organisation role", p)
		}
	}

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
	check("setting", frozen.Settings, func(k string) bool { return slices.Contains(keys, k) }, func(k string) bool { return slices.Contains(appOwned.settings, k) })

	var definitions struct {
		Definitions []struct {
			Name string `json:"name"`
		} `json:"definitions"`
	}
	res = ops.client.Get("/ops/jobs/definitions")
	res.AssertStatus(t, http.StatusOK)
	res.JSON(t, &definitions)
	names := []string{jobs.MailKind}
	for _, d := range definitions.Definitions {
		names = append(names, d.Name)
	}
	check("job", frozen.Jobs, func(j string) bool { return slices.Contains(names, j) }, func(j string) bool { return slices.Contains(appOwned.jobs, j) })

	// /ops/retention lists full-multi's ten rows in its order.
	var retention struct {
		Policies []struct {
			Data    string `json:"data"`
			Setting string `json:"setting"`
			Job     string `json:"job"`
		} `json:"policies"`
	}
	ops.client.Get("/ops/retention").JSON(t, &retention)
	var data []string
	for _, p := range retention.Policies {
		data = append(data, p.Data)
		if p.Data == "deleted_organisations" && (p.Setting != "orgs.deleted_org_retention" || p.Job != "orgs_purge") {
			t.Errorf("deleted_organisations = %+v, want orgs.deleted_org_retention and orgs_purge", p)
		}
	}
	var want []string
	for _, m := range regexp.MustCompile(`\{data: "([a-z_]+)"`).FindAllStringSubmatch(read(t, filepath.Join(goldenMulti, "app", "app.go")), -1) {
		want = append(want, m[1])
	}
	if len(want) != 10 || !slices.Equal(data, want) {
		t.Errorf("GET /ops/retention data = %v, want full-multi's %v", data, want)
	}

	// /ops/auth/rate-limits lists v0.1's limiters, and the invitations
	// limiter full-multi created without listing it.
	var limits struct {
		Limiters []struct {
			Name string `json:"name"`
		} `json:"limiters"`
	}
	ops.client.Get("/ops/auth/rate-limits").JSON(t, &limits)
	var limiterNames []string
	for _, l := range limits.Limiters {
		limiterNames = append(limiterNames, l.Name)
	}
	for _, l := range append(v01RateLimiters(t), [3]string{"orgs_invitations"}) {
		if !slices.Contains(limiterNames, l[0]) {
			t.Errorf("limiter %s isn't listed: %v", l[0], limiterNames)
		}
	}
}
