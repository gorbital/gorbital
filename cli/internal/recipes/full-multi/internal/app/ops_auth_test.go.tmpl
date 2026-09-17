package app_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

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
	var b strings.Builder
	buf := make([]byte, 64<<10)
	for {
		n, err := res.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res.StatusCode, b.String()
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
	if code, body := devDo(t, base, http.MethodGet, "/ops/audit?actor_kind=system", ""); code != http.StatusOK || !strings.Contains(body, "auth.user.banned") || !strings.Contains(body, "auth.user.impersonated") {
		t.Errorf("audit = %d %s", code, body)
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

func TestOpsRateLimits(t *testing.T) {
	_, base := devServer(t, nil)
	code, body := devDo(t, base, http.MethodGet, "/ops/auth/rate-limits", "")
	if code != http.StatusOK || !strings.Contains(body, `"name":"auth_login"`) || !strings.Contains(body, `"keys":`) {
		t.Fatalf("list = %d %s", code, body)
	}
	// A key without a budget resets to nothing; an unknown limiter is 404.
	code, body = devDo(t, base, http.MethodPost, "/ops/auth/rate-limits/reset", `{"name":"auth_ip","key":"203.0.113.9"}`)
	if code != http.StatusOK || !strings.Contains(body, `"reset":false`) {
		t.Errorf("reset unknown key = %d %s", code, body)
	}
	if code, body := devDo(t, base, http.MethodPost, "/ops/auth/rate-limits/reset", `{"name":"nope","key":"x"}`); code != http.StatusNotFound || !strings.Contains(body, "rate_limiter_not_found") {
		t.Errorf("reset unknown limiter = %d %s", code, body)
	}
	if code, body := devDo(t, base, http.MethodGet, "/ops/audit?actor_kind=system", ""); code != http.StatusOK || !strings.Contains(body, "ops.rate_limit.reset") {
		t.Errorf("audit = %d %s", code, body)
	}
}
