package authhttp

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"
)

// devToken is a dev console token as orb dev generates them: 256 bits,
// base64url.
const devToken = "q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E"

// devServer serves an app with the dev console on over a real loopback
// listener and returns the app and its URL.
func devServer(t *testing.T, env map[string]string) (*testApp, string) {
	t.Helper()
	full := map[string]string{"DEV_CONSOLE_TOKEN": devToken}
	maps.Copy(full, env)
	a := newApp(t, full)
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	return a, srv.URL
}

// devGet requests path with the console token; headers are name/value
// pairs, a "Host" pair sets the Host header, and an empty Authorization
// sends none.
func devGet(t *testing.T, base, path string, headers ...string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+devToken)
	for i := 0; i+1 < len(headers); i += 2 {
		switch {
		case headers[i] == "Host":
			req.Host = headers[i+1]
		case headers[i] == "Authorization" && headers[i+1] == "":
			req.Header.Del("Authorization")
		default:
			req.Header.Set(headers[i], headers[i+1])
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(body)
}

// devDo sends a request with the dev console token, which operates /ops/
// in development (ADR-0066), and returns the status and body.
func devDo(t *testing.T, base, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+devToken)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func field(t *testing.T, body, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("not JSON: %s", body)
	}
	v, _ := m[key].(string)
	return v
}

// signInUser signs a user in with their password and returns the
// Authorization header pair and the token.
func signInUser(t *testing.T, h http.Handler, email string) ([]string, string) {
	t.Helper()
	r := do(t, h, http.MethodPost, "/v1/auth/login", `{"email":"`+email+`","password":"correct horse battery staple","transport":"bearer"}`)
	if r.code != http.StatusOK {
		t.Fatalf("login %s = %d %s", email, r.code, r.body)
	}
	token, _ := r.json["token"].(string)
	return []string{"Authorization", "Bearer " + token}, token
}

// systemActions returns the actions of the audit events a system actor
// named id recorded whose action starts with prefix. The golden app read
// them from GET /ops/audit?actor_kind=system, the operations module's
// (Phase 4).
func systemActions(t *testing.T, databaseURL, prefix, id string) []string {
	t.Helper()
	var actions []string
	for _, e := range auditEvents(t, databaseURL, prefix) {
		if e.ActorKind == "system" && e.ActorID == id {
			actions = append(actions, e.Action)
		}
	}
	return actions
}

func TestOpsUsers(t *testing.T) {
	a, base := devServer(t, nil)
	h := a.Handler()

	// Create, list, get.
	code, body := devDo(t, base, http.MethodPost, "/ops/auth/users", `{"email":"ada@example.com","password":"correct horse battery staple","email_verified":true}`)
	if code != http.StatusCreated || !strings.Contains(body, `"email_verified":true`) {
		t.Fatalf("create = %d %s", code, body)
	}
	id := field(t, body, "id")
	code, body = devDo(t, base, http.MethodGet, "/ops/auth/users?q=ada", "")
	if code != http.StatusOK || !strings.Contains(body, id) || strings.Contains(body, "next_cursor") {
		t.Errorf("list = %d %s", code, body)
	}
	code, body = devDo(t, base, http.MethodGet, "/ops/auth/users/"+id, "")
	if code != http.StatusOK || !strings.Contains(body, `"sessions":[]`) || !strings.Contains(body, `"totp":false`) || !strings.Contains(body, `"codes":[]`) {
		t.Errorf("get = %d %s", code, body)
	}
	if code, _ := devDo(t, base, http.MethodGet, "/ops/auth/users/usr_nobody", ""); code != http.StatusNotFound {
		t.Errorf("get missing = %d", code)
	}
	if code, body := devDo(t, base, http.MethodGet, "/ops/auth/users?cursor=nope", ""); code != http.StatusBadRequest || !strings.Contains(body, "invalid_cursor") {
		t.Errorf("bad cursor = %d %s", code, body)
	}

	// A taken address and an unknown role are the caller's mistakes.
	if code, body := devDo(t, base, http.MethodPost, "/ops/auth/users", `{"email":"ada@example.com","password":"correct horse battery staple"}`); code != http.StatusConflict || !strings.Contains(body, "email_taken") {
		t.Errorf("create twice = %d %s", code, body)
	}
	if code, body := devDo(t, base, http.MethodPost, "/ops/auth/users/"+id+"/roles", `{"role":"nope"}`); code != http.StatusUnprocessableEntity || !strings.Contains(body, "unknown_role") {
		t.Errorf("unknown role = %d %s", code, body)
	}

	// Paging: with a second user and limit 1, a cursor continues.
	devDo(t, base, http.MethodPost, "/ops/auth/users", `{"email":"bob@example.com","password":"correct horse battery staple"}`)
	code, body = devDo(t, base, http.MethodGet, "/ops/auth/users?limit=1", "")
	if code != http.StatusOK || !strings.Contains(body, "next_cursor") {
		t.Fatalf("page 1 = %d %s", code, body)
	}
	code, body = devDo(t, base, http.MethodGet, "/ops/auth/users?limit=1&cursor="+field(t, body, "next_cursor"), "")
	if code != http.StatusOK || !strings.Contains(body, "ada@example.com") || strings.Contains(body, "next_cursor") {
		t.Errorf("page 2 = %d %s", code, body)
	}

	// The user signs in; the operator sees the session and ends it.
	headers, _ := signInUser(t, h, "ada@example.com")
	code, body = devDo(t, base, http.MethodGet, "/ops/auth/users/"+id, "")
	if code != http.StatusOK || strings.Contains(body, `"sessions":[]`) {
		t.Fatalf("get with a session = %d %s", code, body)
	}
	if code, body := devDo(t, base, http.MethodDelete, "/ops/auth/users/"+id+"/sessions", ""); code != http.StatusOK || !strings.Contains(body, `"revoked":1`) {
		t.Errorf("revoke sessions = %d %s", code, body)
	}
	if r := do(t, h, http.MethodGet, "/v1/auth/me", "", headers...); r.code != http.StatusUnauthorized {
		t.Errorf("the revoked session still works: %d", r.code)
	}

	// Roles.
	if code, body := devDo(t, base, http.MethodPost, "/ops/auth/users/"+id+"/roles", `{"role":"ops_viewer"}`); code != http.StatusOK || !strings.Contains(body, `"ops_viewer"`) {
		t.Errorf("grant role = %d %s", code, body)
	}
	if code, body := devDo(t, base, http.MethodDelete, "/ops/auth/users/"+id+"/roles/ops_viewer", ""); code != http.StatusOK || strings.Contains(body, `"ops_viewer"`) {
		t.Errorf("revoke role = %d %s", code, body)
	}

	// Ban: sign-in refused, sessions gone; unban restores.
	signInUser(t, h, "ada@example.com")
	if code, _ := devDo(t, base, http.MethodPost, "/ops/auth/users/"+id+"/ban", `{"reason":"spam"}`); code != http.StatusNoContent {
		t.Fatalf("ban = %d", code)
	}
	if r := do(t, h, http.MethodPost, "/v1/auth/login", `{"email":"ada@example.com","password":"correct horse battery staple","transport":"bearer"}`); r.code != http.StatusForbidden || r.json["code"] != "account_banned" {
		t.Errorf("login while banned = %d %s", r.code, r.body)
	}
	_, body = devDo(t, base, http.MethodGet, "/ops/auth/users/"+id, "")
	if !strings.Contains(body, `"banned_reason":"spam"`) || !strings.Contains(body, `"sessions":[]`) {
		t.Errorf("banned user = %s", body)
	}
	if code, _ := devDo(t, base, http.MethodPost, "/ops/auth/users/"+id+"/unban", ""); code != http.StatusNoContent {
		t.Errorf("unban = %d", code)
	}
	if r := do(t, h, http.MethodPost, "/v1/auth/login", `{"email":"ada@example.com","password":"correct horse battery staple","transport":"bearer"}`); r.code != http.StatusOK {
		t.Errorf("login after unban = %d %s", r.code, r.body)
	}

	// Impersonation works in development and acts as the user.
	code, body = devDo(t, base, http.MethodPost, "/ops/auth/users/"+id+"/impersonate", `{}`)
	if code != http.StatusCreated {
		t.Fatalf("impersonate = %d %s", code, body)
	}
	token := field(t, body, "token")
	if r := do(t, h, http.MethodGet, "/v1/auth/me", "", "Authorization", "Bearer "+token); r.code != http.StatusOK || !strings.Contains(r.body, id) {
		t.Errorf("impersonated /v1/auth/me = %d %s", r.code, r.body)
	}

	// MFA on and reset; the audit log names the operator.
	if code, body := devDo(t, base, http.MethodPost, "/ops/auth/users/"+id+"/mfa/enroll", ""); code != http.StatusCreated || !strings.Contains(body, "recovery_codes") {
		t.Errorf("enroll = %d %s", code, body)
	}
	if code, body := devDo(t, base, http.MethodGet, "/ops/auth/users/"+id, ""); !strings.Contains(body, `"totp":true`) {
		t.Errorf("mfa status = %d %s", code, body)
	}
	if code, _ := devDo(t, base, http.MethodPost, "/ops/auth/users/"+id+"/mfa/reset", ""); code != http.StatusNoContent {
		t.Errorf("reset mfa = %d", code)
	}
	if actions := systemActions(t, a.url, "auth.", "dev-console"); !slices.Contains(actions, "auth.user.banned") || !slices.Contains(actions, "auth.user.impersonated") {
		t.Errorf("audit events by system/dev-console = %v, want auth.user.banned and auth.user.impersonated", actions)
	}

	// Delete.
	if code, _ := devDo(t, base, http.MethodDelete, "/ops/auth/users/"+id, ""); code != http.StatusNoContent {
		t.Errorf("delete = %d", code)
	}
	if code, _ := devDo(t, base, http.MethodGet, "/ops/auth/users/"+id, ""); code != http.StatusNotFound {
		t.Errorf("get after delete = %d", code)
	}

	// Without the operator, the endpoints need a signed-in administrator.
	req, _ := http.NewRequest(http.MethodGet, base+"/ops/auth/users", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous list = %d", res.StatusCode)
	}
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")
	if r := do(t, h, http.MethodGet, "/ops/auth/users", "", viewer...); r.code != http.StatusOK {
		t.Errorf("ops_viewer list = %d %s", r.code, r.body)
	}
	if r := do(t, h, http.MethodPost, "/ops/auth/users/"+id+"/ban", `{}`, viewer...); r.code != http.StatusForbidden {
		t.Errorf("ops_viewer ban = %d %s", r.code, r.body)
	}
}

func TestOpsImpersonationOffWithoutTheConsole(t *testing.T) {
	a := newApp(t, nil)
	admin, _ := signIn(t, a, "admin@example.com", "platform_admin")
	r := do(t, a.Handler(), http.MethodPost, "/ops/auth/users", `{"email":"eve@example.com","password":"correct horse battery staple"}`, admin...)
	if r.code != http.StatusCreated {
		t.Fatalf("create = %d %s", r.code, r.body)
	}
	id, _ := r.json["id"].(string)
	if r := do(t, a.Handler(), http.MethodPost, "/ops/auth/users/"+id+"/impersonate", `{}`, admin...); r.code != http.StatusForbidden || r.json["code"] != "impersonation_off" {
		t.Errorf("impersonate without the console = %d %s", r.code, r.body)
	}
}

// TestOpsRateLimits (GET /ops/auth/rate-limits, POST
// /ops/auth/rate-limits/reset) moves with /ops in Phase 4.

// TestDevOperator is the golden app's, on sign-in's /ops endpoints: it used
// /ops/settings as "an /ops endpoint" and /ops/audit to find the change,
// which are the operations module's (Phase 4).
func TestDevOperator(t *testing.T) {
	a, base := devServer(t, nil)
	port := base[strings.LastIndex(base, ":")+1:]

	// The console token operates /ops/ in development, as a system actor.
	code, _, body := devGet(t, base, "/ops/auth/users")
	if code != http.StatusOK || !strings.Contains(body, `"users":[`) {
		t.Fatalf("GET /ops/auth/users with the console token = %d %s, want 200", code, body)
	}
	req, _ := http.NewRequest(http.MethodPost, base+"/ops/auth/users", strings.NewReader(`{"email":"portal@example.com","password":"correct horse battery staple"}`))
	req.Header.Set("Authorization", "Bearer "+devToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Errorf("POST /ops/auth/users as the operator = %d, want 201", res.StatusCode)
	}
	if actions := systemActions(t, a.url, "auth.user.created", "dev-console"); len(actions) == 0 {
		t.Errorf("the change wasn't audited as system/dev-console: %+v", auditEvents(t, a.url, "auth.user.created"))
	}

	// Never outside /ops/, never with the wrong Host, never in a cookie.
	if code, _, body := devGet(t, base, "/v1/auth/me"); code != http.StatusUnauthorized {
		t.Errorf("GET /v1/auth/me with the console token = %d %s, want 401", code, body)
	}
	for _, host := range []string{"evil.example:" + port, "localhost:1"} {
		if code, _, _ := devGet(t, base, "/ops/auth/users", "Host", host); code != http.StatusUnauthorized {
			t.Errorf("Host %s: GET /ops/auth/users = %d, want 401 (the operator needs a localhost Host)", host, code)
		}
	}
	if code, _, _ := devGet(t, base, "/ops/auth/users", "Authorization", "", "Cookie", authlib.DefaultCookieName+"="+devToken); code != http.StatusUnauthorized {
		t.Errorf("token in the session cookie = %d, want 401", code)
	}
	if code, _, _ := devGet(t, base, "/ops/auth/users", "Authorization", "Bearer "+devToken[1:]); code != http.StatusUnauthorized {
		t.Errorf("a wrong token = %d, want 401", code)
	}

	// Without the console there is no operator: the token is just an unknown one.
	srv := httptest.NewServer(newApp(t, nil).Handler())
	defer srv.Close()
	if code, _, _ := devGet(t, srv.URL, "/ops/auth/users"); code != http.StatusUnauthorized {
		t.Errorf("GET /ops/auth/users with the token but no console = %d, want 401", code)
	}
}

// TestDevConsoleMailPreviews checks what sign-in gives the dev console:
// previews of its branded emails beside the console's test message, and
// the /.well-known files for passkeys in native apps among the plain
// handlers /_dev/routes lists, each really served.
func TestDevConsoleMailPreviews(t *testing.T) {
	fingerprint := passkey.FormatFingerprint(sha256.Sum256([]byte("dev console signing certificate")))
	_, base := devServer(t, map[string]string{
		"WEBAUTHN_APPLE_APP_IDS": "ABCDE12345.com.example.app",
		"WEBAUTHN_ANDROID_APPS":  "com.example.app=SHA256:" + fingerprint,
	})

	code, _, body := devGet(t, base, "/_dev/mail/previews")
	var previews struct {
		Previews []struct{ Name, Description string } `json:"previews"`
	}
	if err := json.Unmarshal([]byte(body), &previews); err != nil || code != http.StatusOK {
		t.Fatalf("/_dev/mail/previews = %d %s", code, body)
	}
	var auth, test int
	for _, p := range previews.Previews {
		switch {
		case p.Name == "test":
			test++
		case strings.HasPrefix(p.Name, "auth."):
			auth++
		default:
			t.Errorf("preview %q is neither sign-in's nor the test message", p.Name)
		}
		if p.Description == "" {
			t.Errorf("preview %q has no description", p.Name)
		}
	}
	if auth == 0 || test != 1 {
		t.Errorf("/_dev/mail/previews has %d sign-in previews and %d test messages, want some and one: %s", auth, test, body)
	}
	for _, p := range previews.Previews {
		if code, _, rendered := devGet(t, base, "/_dev/mail/preview?name="+p.Name); code != http.StatusOK || !strings.Contains(rendered, `"subject":"`) {
			t.Errorf("/_dev/mail/preview?name=%s = %d %s", p.Name, code, rendered)
		}
	}

	code, _, body = devGet(t, base, "/_dev/routes")
	var list struct {
		Routes []struct {
			Method, Path, Source string
		} `json:"routes"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil || code != http.StatusOK {
		t.Fatalf("/_dev/routes = %d %s", code, body)
	}
	want := map[string]string{
		"/.well-known/apple-app-site-association": "ABCDE12345.com.example.app",
		"/.well-known/assetlinks.json":            fingerprint,
	}
	for _, r := range list.Routes {
		if r.Source != "handler" || r.Method != http.MethodGet {
			continue // POST handlers (the passkey test's steps) need a body
		}
		// Every plain GET handler listed is really served.
		code, _, served := devGet(t, base, r.Path)
		if code == http.StatusNotFound {
			t.Errorf("listed route %s %s isn't served: %s", r.Method, r.Path, served)
		}
		if content, ok := want[r.Path]; ok {
			delete(want, r.Path)
			if r.Method != http.MethodGet || code != http.StatusOK || !strings.Contains(served, content) {
				t.Errorf("%s %s = %d %s, want 200 with %s", r.Method, r.Path, code, served, content)
			}
		}
	}
	for path := range want {
		t.Errorf("/_dev/routes doesn't list %s as a handler: %s", path, body)
	}
	if !strings.Contains(body, `"path":"/ops/auth/users"`) || !strings.Contains(body, `"path":"/v1/auth/login"`) {
		t.Errorf("/_dev/routes lacks sign-in's operations: %s", body)
	}
}
