package authhttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/settings"
)

// The contract tests compare sign-in in the library with the frozen v0.1.0
// contracts (internal/contracts/v0.1.0) and with the v0.1 golden app's
// source, which is unchanged since the v0.1.0 tag
// (examples/full-single/internal/modules/auth and internal/app).

var (
	repo      = filepath.Join("..", "..")
	frozen    = filepath.Join(repo, "internal", "contracts", "v0.1.0", "examples", "full-single", "api")
	goldenApp = filepath.Join(repo, "examples", "full-single")
)

// signInPrefixes are the paths of v0.1's OpenAPI document that sign-in
// serves.
var signInPrefixes = []string{"/v1/auth/", "/ops/auth/users", "/ops/service-accounts"}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestOpenAPIMatchesV010: every sign-in operation of the frozen v0.1.0
// document is in the app's document with the same operation ID, security,
// parameters, request body and responses, the schemas they reference are
// identical, and there are no others under those paths. Only gorbital's
// x-gorbital-guards extension is new. openapi.CheckCompatible, which CI
// runs for the golden apps, passes too.
func TestOpenAPIMatchesV010(t *testing.T) {
	baseline := readFile(t, filepath.Join(frozen, "openapi.json"))
	r := do(t, newApp(t, nil).Handler(), "GET", "/openapi.json", "")
	if r.code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d", r.code)
	}
	current := []byte(r.body)

	for _, prefix := range signInPrefixes {
		found, err := openapi.CheckCompatible(baseline, current, prefix)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range found {
			t.Errorf("breaking change to %s* compared with v0.1.0: %s", prefix, f)
		}
	}

	var base, cur openAPIDoc
	if err := json.Unmarshal(baseline, &base); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(current, &cur); err != nil {
		t.Fatal(err)
	}
	signIn := func(path string) bool {
		return slices.ContainsFunc(signInPrefixes, func(p string) bool { return strings.HasPrefix(path, p) })
	}
	refs := map[string]bool{}
	operations := 0
	for path, item := range base.Paths {
		if !signIn(path) {
			continue
		}
		for method, op := range item {
			operations++
			got, ok := cur.Paths[path][method]
			if !ok {
				t.Errorf("%s %s is missing", method, path)
				continue
			}
			var guards struct {
				Guards   []string `json:"x-gorbital-guards"`
				Security []any    `json:"security"`
			}
			if err := json.Unmarshal(got, &guards); err != nil {
				t.Fatal(err)
			}
			want := []string{"public"}
			if len(guards.Security) > 0 {
				want = []string{"authenticated"}
			}
			if !slices.Equal(guards.Guards, want) {
				t.Errorf("%s %s: x-gorbital-guards = %v, want %v", method, path, guards.Guards, want)
			}
			if a, b := canonical(t, op, "x-gorbital-guards"), canonical(t, got, "x-gorbital-guards"); a != b {
				t.Errorf("%s %s changed:\nv0.1.0: %s\nnow:    %s", method, path, a, b)
			}
			collectRefs(t, op, refs)
		}
	}
	for path, item := range cur.Paths {
		for method := range item {
			if _, ok := base.Paths[path][method]; signIn(path) && !ok {
				t.Errorf("%s %s is new under sign-in's paths", method, path)
			}
		}
	}
	if operations != 66 {
		t.Errorf("compared %d operations, want v0.1.0's 66", operations)
	}
	// Schemas referenced by those operations, and by those schemas.
	for seen := 0; seen != len(refs); {
		seen = len(refs)
		for name := range refs {
			collectRefs(t, base.Components.Schemas[name], refs)
		}
	}
	for name := range refs {
		if a, b := canonical(t, base.Components.Schemas[name]), canonical(t, cur.Components.Schemas[name]); a != b {
			t.Errorf("schema %s changed:\nv0.1.0: %s\nnow:    %s", name, a, b)
		}
	}
	if a, b := canonical(t, base.Components.SecuritySchemes), canonical(t, cur.Components.SecuritySchemes); a != b {
		t.Errorf("security schemes changed:\nv0.1.0: %s\nnow:    %s", a, b)
	}
}

type openAPIDoc struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas         map[string]json.RawMessage `json:"schemas"`
		SecuritySchemes json.RawMessage            `json:"securitySchemes"`
	} `json:"components"`
}

// canonical re-encodes JSON with sorted keys, without the keys named in
// drop at the top level.
func canonical(t *testing.T, raw json.RawMessage, drop ...string) string {
	t.Helper()
	if raw == nil {
		return "null"
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if m, ok := v.(map[string]any); ok {
		for _, k := range drop {
			delete(m, k)
		}
	}
	out, err := json.Marshal(v) // maps marshal with sorted keys
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

var schemaRef = regexp.MustCompile(`"#/components/schemas/([^"]+)"`)

func collectRefs(t *testing.T, raw json.RawMessage, refs map[string]bool) {
	t.Helper()
	for _, m := range schemaRef.FindAllStringSubmatch(string(raw), -1) {
		refs[m[1]] = true
	}
}

// frozenSurface returns the names of v0.1.0's surface.json.
func frozenSurface(t *testing.T) (s struct {
	ErrorCodes   []string            `json:"error_codes"`
	AuditActions []string            `json:"audit_actions"`
	Permissions  map[string][]string `json:"permissions"`
	Roles        map[string][]string `json:"roles"`
	Settings     []string            `json:"settings"`
	Jobs         []string            `json:"jobs"`
}) {
	t.Helper()
	if err := json.Unmarshal(readFile(t, filepath.Join(frozen, "surface.json")), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// goSources returns the non-test Go files under dir, concatenated.
func goSources(t *testing.T, dirs ...string) string {
	t.Helper()
	var b strings.Builder
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				b.Write(readFile(t, path))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return b.String()
}

var (
	mappedCode  = regexp.MustCompile(`Code: "([a-z0-9_]+)"`)
	problemCode = regexp.MustCompile(`httpx\.NewProblem\(http\.Status[A-Za-z]+, "([a-z0-9_]+)"`)
	auditAction = regexp.MustCompile(`"(auth\.[a-z_]+\.[a-z_]+)"`)
	limiterName = regexp.MustCompile(`\{"(auth_[a-z_]+)", &limits\.`)
)

func matches(re *regexp.Regexp, s string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// TestSurfaceKeepsV010Names: sign-in's error codes, audit actions,
// permissions, roles, runtime settings, jobs and rate limiters are a
// superset of v0.1.0's.
func TestSurfaceKeepsV010Names(t *testing.T) {
	frozen := frozenSurface(t)
	golden := goSources(t, filepath.Join(goldenApp, "internal", "modules", "auth"), filepath.Join(goldenApp, "internal", "app", "module_auth.go"))
	library := goSources(t, ".")

	// Error codes: what v0.1's sign-in maps or writes.
	codes := map[string]bool{}
	for _, m := range New().Module().Errors {
		codes[m.Code] = true
	}
	for _, c := range matches(problemCode, library) {
		codes[c] = true
	}
	v01Codes := slices.Concat(matches(mappedCode, golden), matches(problemCode, golden))
	if len(v01Codes) < 40 {
		t.Fatalf("found %d error codes in v0.1's sign-in, want every one", len(v01Codes))
	}
	for _, c := range v01Codes {
		if !slices.Contains(frozen.ErrorCodes, c) {
			t.Errorf("error code %q of v0.1's sign-in isn't in the v0.1.0 surface: fix the scan", c)
		}
		if !codes[c] {
			t.Errorf("error code %q of v0.1.0 no longer exists in authhttp", c)
		}
	}

	// Audit actions: every auth.* action of v0.1.0 is written by authhttp.
	actions := matches(auditAction, library)
	for _, a := range frozen.AuditActions {
		if strings.HasPrefix(a, "auth.") && !slices.Contains(actions, a) {
			t.Errorf("audit action %q of v0.1.0 isn't recorded by authhttp", a)
		}
	}

	// Permissions and roles.
	var perms []string
	for _, p := range New().Module().Permissions {
		perms = append(perms, p.Name)
	}
	for _, p := range frozen.Permissions["platform"] {
		if (strings.HasPrefix(p, "ops.auth.") || strings.HasPrefix(p, "ops.service_accounts.")) && !slices.Contains(perms, p) {
			t.Errorf("permission %q of v0.1.0 isn't declared by authhttp", p)
		}
	}
	var roles []string
	for _, r := range newApp(t, nil).Auth().Catalog().Roles() {
		roles = append(roles, r.Name)
	}
	for _, r := range frozen.Roles["platform"] {
		if !slices.Contains(roles, r) {
			t.Errorf("role %q of v0.1.0 isn't in an app with authhttp: %v", r, roles)
		}
	}

	// Settings: auth.ip_requests_per_minute is gorbital's stack.
	reg := settings.NewRegistry()
	New().Module().Settings(reg)
	keys := append(reg.Keys(), "auth.ip_requests_per_minute")
	for _, k := range frozen.Settings {
		if strings.HasPrefix(k, "auth.") && !slices.Contains(keys, k) {
			t.Errorf("setting %q of v0.1.0 isn't declared by authhttp", k)
		}
	}

	// Jobs.
	defs := jobs.NewDefinitions()
	New().Module().Jobs(defs, gorbital.Deps{})
	for _, name := range []string{"auth_cleanup", "auth_revoke_tokens"} {
		if !slices.Contains(frozen.Jobs, name) || !slices.Contains(defs.Names(), name) {
			t.Errorf("job %q: in v0.1.0 %v, defined by authhttp %v", name, frozen.Jobs, defs.Names())
		}
	}

	// Rate limiters, by name: auth_ip is gorbital's stack.
	v01Limiters := matches(limiterName, string(readFile(t, filepath.Join(goldenApp, "internal", "app", "rate_limits.go"))))
	names := []string{"auth_ip"}
	for _, l := range limiters {
		names = append(names, l.name)
	}
	names = append(names, matches(limiterName, string(readFile(t, "limits.go")))...)
	if len(v01Limiters) != 8 {
		t.Fatalf("found v0.1 limiters %v, want 8", v01Limiters)
	}
	for _, name := range v01Limiters {
		if !slices.Contains(names, name) {
			t.Errorf("rate limiter %q of v0.1.0 isn't created by authhttp", name)
		}
	}
	for _, l := range limiters {
		if !slices.Contains(matches(limiterName, string(readFile(t, "limits.go"))), l.name) {
			t.Errorf("limiter %q is listed but not created", l.name)
		}
	}
}

// TestModuleMigrationsMatchV01Apps: sign-in's migrations are byte for byte
// the files the v0.1 golden apps hold under the same versions.
func TestModuleMigrationsMatchV01Apps(t *testing.T) {
	for _, app := range []string{"full-single", "full-multi"} {
		for _, m := range moduleMigrations() {
			name := strconv.FormatInt(m.Version, 10) + "_" + m.Name + ".sql"
			want := readFile(t, filepath.Join(repo, "examples", app, "db", "migrations", name))
			got, err := fs.ReadFile(m.FS, m.File)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s differs from examples/%s/db/migrations/%s: released migrations never change", m.File, app, name)
			}
		}
	}
}

// TestCookies: the session cookie is __Host-session with the attributes of
// v0.1 (HttpOnly, Secure, SameSite=Lax, Path=/, no Domain), cleared the
// same way at sign-out.
func TestCookies(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	signIn(t, a, "ada@example.com", "")
	r := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":"ada@example.com","password":%q}`, testPassword))
	cookies := r.header.Values("Set-Cookie")
	if r.code != http.StatusOK || len(cookies) != 1 {
		t.Fatalf("login = %d %v", r.code, cookies)
	}
	c, err := http.ParseSetCookie(cookies[0])
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "__Host-session" || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.Expires.IsZero() {
		t.Errorf("session cookie = %q, want __Host-session; Path=/; Expires; HttpOnly; Secure; SameSite=Lax", cookies[0])
	}
	session := []string{"Cookie", c.Name + "=" + c.Value}
	out := do(t, h, "POST", "/v1/auth/logout", "", session...)
	cleared, err := http.ParseSetCookie(out.header.Get("Set-Cookie"))
	if err != nil || out.code != http.StatusNoContent || cleared.Name != "__Host-session" || cleared.MaxAge >= 0 || cleared.Value != "" ||
		!cleared.HttpOnly || !cleared.Secure || cleared.SameSite != http.SameSiteLaxMode || cleared.Path != "/" {
		t.Errorf("logout = %d, Set-Cookie %q, want the cookie cleared with the same attributes", out.code, out.header.Get("Set-Cookie"))
	}
}

// TestSignedInRoutesRefuseAnonymous: sign-in's signed-in operations leave
// the actor check to their use cases, after the input is validated, as in
// v0.1 (route.Config.ActorCheckedByHandler). Every one of them, called
// without credentials, with no body and with an empty object, answers 401
// unauthenticated or, for a missing or invalid body, 400 bad_request or 422
// validation_failed: never a success, another refusal or a server error. A
// valid body then reaches the use case's refusal, which the golden app's
// HTTP tests exercise route by route.
func TestSignedInRoutesRefuseAnonymous(t *testing.T) {
	a := newApp(t, socialEnvForContracts(t))
	h := a.Handler()
	var doc openAPIDoc
	r := do(t, h, "GET", "/openapi.json", "")
	if err := json.Unmarshal([]byte(r.body), &doc); err != nil {
		t.Fatal(err)
	}
	checked, invalid := 0, 0
	for path, item := range doc.Paths {
		if !strings.HasPrefix(path, "/v1/auth/") && !strings.HasPrefix(path, "/ops/") {
			continue
		}
		for method, raw := range item {
			var op struct {
				Security []any `json:"security"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				t.Fatal(err)
			}
			if len(op.Security) == 0 {
				continue
			}
			checked++
			url := strings.NewReplacer("{id}", "usr_x", "{provider}", "github", "{sessionId}", "ses_x", "{passkeyId}", "pk_x",
				"{identityId}", "idn_x", "{role}", "ops_viewer", "{keyId}", "key_x").Replace(path)
			for _, body := range []string{"", "{}"} {
				got := do(t, h, strings.ToUpper(method), url, body)
				code, _ := got.json["code"].(string)
				switch {
				case got.code == http.StatusUnauthorized && code == "unauthenticated":
				case got.code == http.StatusUnprocessableEntity && code == "validation_failed", got.code == http.StatusBadRequest && code == "bad_request":
					invalid++
				default:
					t.Errorf("%s %s without credentials, body %q = %d %s, want 401 unauthenticated, or 422 validation_failed or 400 bad_request for the body", strings.ToUpper(method), url, body, got.code, got.body)
				}
			}
		}
	}
	if checked < 40 || invalid >= 2*checked {
		t.Errorf("checked %d signed-in operations, %d answers about the body, want every operation and most refused as unauthenticated", checked, invalid)
	}
	t.Logf("%d signed-in operations; %d of %d answers were about the body", checked, invalid, 2*checked)
}

// socialEnvForContracts turns on GitHub sign-in, so its signed-in link
// operation runs its use case instead of answering social_unavailable.
func socialEnvForContracts(*testing.T) map[string]string {
	return map[string]string{"GITHUB_CLIENT_ID": "client", "GITHUB_CLIENT_SECRET": "secret"}
}
