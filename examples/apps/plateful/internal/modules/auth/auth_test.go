package authhttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	authlib "gorbital.dev/modules/auth"
)

var sixDigits = regexp.MustCompile(`\b(\d{6})\b`)

// emailedCode returns the newest 6-digit code queued in an email to to. Email
// is queued as a job, so the code is in the job's arguments.
func emailedCode(t *testing.T, pool *pgxpool.Pool, to string) string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT args FROM river_job WHERE kind = 'gorbital.mail.send' ORDER BY id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var args struct {
			Message struct {
				To   []struct{ Email string } `json:"to"`
				Text string                   `json:"text"`
			} `json:"message"`
		}
		if json.Unmarshal(raw, &args) != nil || len(args.Message.To) == 0 || args.Message.To[0].Email != to {
			continue
		}
		if m := sixDigits.FindStringSubmatch(args.Message.Text); m != nil {
			return m[1]
		}
	}
	t.Fatalf("no emailed code for %s", to)
	return ""
}

func cookieHeader(t *testing.T, r response) []string {
	t.Helper()
	for _, c := range r.header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "__Host-session=") {
			value, _, _ := strings.Cut(strings.TrimPrefix(c, "__Host-session="), ";")
			return []string{"Cookie", "__Host-session=" + value}
		}
	}
	t.Fatalf("no session cookie in %v", r.header.Values("Set-Cookie"))
	return nil
}

// TestAuthenticationEndToEnd follows the basic sign-in flow: register, verify the
// emailed code, sign in, reach a role-protected endpoint once granted the
// role, and find the audit events.
func TestAuthenticationEndToEnd(t *testing.T) {
	a, url := newAppWithURL(t, nil)
	h := a.Handler()
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	const email, password = "ada@example.com", "a long enough password"
	creds := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)

	if r := do(t, h, "POST", "/v1/auth/register", `{"email":"ada@example.com","password":"short"}`); r.code != 422 || r.json["code"] != "weak_password" ||
		!strings.Contains(r.json["detail"].(string), "at least 12") {
		t.Errorf("register with a short password = %d %s", r.code, r.body)
	}
	for range 2 { // a second registration looks the same
		if r := do(t, h, "POST", "/v1/auth/register", creds); r.code != http.StatusAccepted || r.json["status"] != "check_your_email" {
			t.Fatalf("register = %d %s", r.code, r.body)
		}
	}
	if r := do(t, h, "POST", "/v1/auth/login", creds); r.code != http.StatusForbidden || r.json["code"] != "email_not_verified" {
		t.Errorf("login before verifying = %d %s", r.code, r.body)
	}
	code := emailedCode(t, pool, email)
	if r := do(t, h, "POST", "/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":"000000x"}`, email)); r.code != 422 || r.json["code"] != "invalid_code" {
		t.Errorf("verify with a wrong code = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/verify-email", fmt.Sprintf(`{"email":%q,"code":%q}`, email, code)); r.code != http.StatusNoContent {
		t.Fatalf("verify = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"a wrong long password"}`, email)); r.code != 401 || r.json["code"] != "invalid_credentials" {
		t.Errorf("login with a wrong password = %d %s", r.code, r.body)
	}

	// Browser sign-in: an HttpOnly cookie, no token in the body.
	browserLogin := do(t, h, "POST", "/v1/auth/login", creds, "User-Agent", "Firefox/140")
	setCookie := browserLogin.header.Get("Set-Cookie")
	if browserLogin.code != http.StatusOK || browserLogin.json["token"] != nil ||
		!strings.Contains(setCookie, "HttpOnly") || !strings.Contains(setCookie, "Secure") || !strings.Contains(setCookie, "SameSite=Lax") {
		t.Fatalf("cookie login = %d %s, Set-Cookie %q", browserLogin.code, browserLogin.body, setCookie)
	}
	browser := cookieHeader(t, browserLogin)
	me := do(t, h, "GET", "/v1/auth/me", "", browser...)
	user, _ := me.json["user"].(map[string]any)
	if me.code != http.StatusOK || user["email"] != email || user["email_verified"] != true {
		t.Fatalf("GET /v1/auth/me with the cookie = %d %s", me.code, me.body)
	}
	userID := user["id"].(string)
	if r := do(t, h, "POST", "/v1/auth/logout", "", append(browser, "Sec-Fetch-Site", "cross-site")...); r.code != http.StatusForbidden {
		t.Errorf("cross-site POST with the cookie = %d %s, want 403", r.code, r.body)
	}

	// Native sign-in: the token in the body.
	nativeLogin := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q,"transport":"bearer"}`, email, password))
	token, _ := nativeLogin.json["token"].(string)
	if nativeLogin.code != http.StatusOK || token == "" || nativeLogin.header.Get("Set-Cookie") != "" {
		t.Fatalf("bearer login = %d %s", nativeLogin.code, nativeLogin.body)
	}
	native := []string{"Authorization", "Bearer " + token}
	sessions := do(t, h, "GET", "/v1/auth/sessions", "", native...)
	if list, _ := sessions.json["sessions"].([]any); len(list) != 2 {
		t.Errorf("GET /v1/auth/sessions = %s, want 2 sessions", sessions.body)
	}

	// Role-protected endpoint: forbidden until an operator grants the role.
	// (v0.1's test reads /ops/audit, the operations module's; sign-in's own
	// /ops/auth/users needs the same role.)
	if r := do(t, h, "GET", "/ops/auth/users", "", native...); r.code != http.StatusForbidden {
		t.Errorf("GET /ops/auth/users without a role = %d %s, want 403", r.code, r.body)
	}
	if out, err := runCommand(t, a, "grant-role", email, "ops_viewer"); err != nil || !strings.Contains(out, "ops_viewer") {
		t.Fatalf("grant-role = %q, %v", out, err)
	}
	if _, err := runCommand(t, a, "grant-role", email, "superuser"); err == nil || !strings.Contains(err.Error(), "platform_admin") {
		t.Errorf("grant-role (unknown role) error = %v, want the list of roles", err)
	}
	if _, err := runCommand(t, a, "grant-role", "nobody@example.com", "ops_viewer"); err == nil || !strings.Contains(err.Error(), "register") {
		t.Errorf("grant-role (unknown account) error = %v", err)
	}
	// Roles go only to verified accounts: whoever verifies an address later
	// may not be who registered it.
	do(t, h, "POST", "/v1/auth/register", `{"email":"pending@example.com","password":"a long enough password"}`)
	if _, err := runCommand(t, a, "grant-role", "pending@example.com", "ops_viewer"); err == nil || !strings.Contains(err.Error(), "hasn't verified its email address") {
		t.Errorf("grant-role (unverified account) error = %v, want a refusal naming verification", err)
	}
	// The role requires two-factor authentication, which this account hasn't
	// turned on (ADR-0043).
	if r := do(t, h, "GET", "/ops/auth/users", "", native...); r.code != http.StatusForbidden || r.json["code"] != "mfa_required" {
		t.Errorf("GET /ops/auth/users as ops_viewer without 2FA = %d %s, want 403 mfa_required", r.code, r.body)
	}
	found := false
	for _, ev := range auditEvents(t, url, "auth.") {
		if ev.Action == "auth.login.succeeded" && ev.ActorID == userID && ev.UserAgent == "Firefox/140" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit events = %+v, want auth.login.succeeded with the browser's user agent", auditEvents(t, url, "auth."))
	}
	if p := do(t, h, "GET", "/v1/auth/me", "", native...); !strings.Contains(p.body, "ops.auth.read") {
		t.Errorf("GET /v1/auth/me after the grant = %s, want ops permissions", p.body)
	}

	// Change password: other sessions end, this one stays.
	if r := do(t, h, "PUT", "/v1/auth/password", `{"current_password":"wrong password here","new_password":"a brand new password"}`, native...); r.code != 401 || r.json["code"] != "invalid_credentials" {
		t.Errorf("change password with a wrong current password = %d %s", r.code, r.body)
	}
	if r := do(t, h, "PUT", "/v1/auth/password", fmt.Sprintf(`{"current_password":%q,"new_password":"a brand new password"}`, password), native...); r.code != http.StatusNoContent {
		t.Fatalf("change password = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/auth/me", "", browser...); r.code != 401 {
		t.Errorf("cookie session after changing the password = %d, want signed out", r.code)
	}
	if r := do(t, h, "GET", "/v1/auth/me", "", native...); r.code != http.StatusOK {
		t.Errorf("current session after changing the password = %d, want kept", r.code)
	}

	// Password reset: uniform response, then the emailed code.
	for _, addr := range []string{"nobody@example.com", email} {
		if r := do(t, h, "POST", "/v1/auth/password/forgot", fmt.Sprintf(`{"email":%q}`, addr)); r.code != http.StatusAccepted {
			t.Errorf("forgot password for %s = %d %s", addr, r.code, r.body)
		}
	}
	reset := fmt.Sprintf(`{"email":%q,"code":%q,"password":"the third password"}`, email, emailedCode(t, pool, email))
	if r := do(t, h, "POST", "/v1/auth/password/reset", reset); r.code != http.StatusNoContent {
		t.Fatalf("reset password = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/auth/me", "", native...); r.code != 401 {
		t.Errorf("session after a reset = %d, want signed out", r.code)
	}

	third := fmt.Sprintf(`{"email":%q,"password":"the third password","transport":"bearer"}`, email)
	first, second := do(t, h, "POST", "/v1/auth/login", third), do(t, h, "POST", "/v1/auth/login", third)
	firstAuth := []string{"Authorization", "Bearer " + first.json["token"].(string)}
	secondAuth := []string{"Authorization", "Bearer " + second.json["token"].(string)}
	secondID := second.json["session"].(map[string]any)["id"].(string)
	if r := do(t, h, "DELETE", "/v1/auth/sessions/"+secondID, "", firstAuth...); r.code != http.StatusNoContent {
		t.Errorf("revoke session = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/auth/sessions/ses_unknown", "", firstAuth...); r.code != 404 || r.json["code"] != "session_not_found" {
		t.Errorf("revoke unknown session = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/auth/me", "", secondAuth...); r.code != 401 {
		t.Errorf("revoked session = %d, want 401", r.code)
	}

	// Delete the account.
	if r := do(t, h, "DELETE", "/v1/auth/me", `{"password":"not the password"}`, firstAuth...); r.code != 401 {
		t.Errorf("delete account with a wrong password = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/auth/me", `{"password":"the third password"}`, firstAuth...); r.code != http.StatusNoContent || !strings.Contains(r.header.Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("delete account = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/login", third); r.code != 401 {
		t.Errorf("login to a deleted account = %d, want 401", r.code)
	}
}

func TestLogout(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	bearer, _ := signIn(t, a, "ada@example.com", "")
	if r := do(t, h, "POST", "/v1/auth/logout", "", bearer...); r.code != http.StatusNoContent || !strings.Contains(r.header.Get("Set-Cookie"), "Max-Age=0") {
		t.Errorf("logout = %d %s, Set-Cookie %q", r.code, r.body, r.header.Get("Set-Cookie"))
	}
	if r := do(t, h, "GET", "/v1/auth/me", "", bearer...); r.code != 401 || r.json["code"] != "unauthenticated" {
		t.Errorf("GET /v1/auth/me after logout = %d %s", r.code, r.body)
	}
	other, _ := signIn(t, a, "bob@example.com", "")
	if r := do(t, h, "POST", "/v1/auth/logout-all", "", other...); r.code != http.StatusOK || r.json["revoked"] != float64(1) {
		t.Errorf("logout-all = %d %s", r.code, r.body)
	}
	if out, err := runCommand(t, a, "roles"); err != nil || !strings.Contains(out, "platform_admin") || !strings.Contains(out, "ops.auth.write") {
		t.Errorf("roles = %s, %v", out, err)
	}
}

// TestChecksBehindASessionAreLimited is the reviewers' proof of concept over
// HTTP: with a stolen session and a new client address for every request,
// the password can't be guessed through the changes that check it (security
// review AUTH-S-5, AUTH-M-2).
func TestChecksBehindASessionAreLimited(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	const email = "ada@example.com"
	if _, err := a.Auth().CreateUser(actor.With(context.Background(), actor.System("test")), email, testPassword, true); err != nil {
		t.Fatal(err)
	}
	session := cookieHeader(t, do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, testPassword)))
	guesses := []struct{ method, path, body string }{
		{"POST", "/v1/auth/mfa/totp", `{"password":"wrong password %d"}`},
		{"PUT", "/v1/auth/password", `{"current_password":"wrong password %d","new_password":"a new long password"}`},
		{"POST", "/v1/auth/passkeys/registration", `{"password":"wrong password %d"}`},
		{"DELETE", "/v1/auth/me", `{"password":"wrong password %d"}`},
	}
	for i := range authlib.DefaultLoginAttempts {
		g := guesses[i%len(guesses)]
		if r := fromClient(t, h, g.method, g.path, fmt.Sprintf(g.body, i), fmt.Sprintf("198.51.100.%d:1234", i), session...); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_credentials" {
			t.Fatalf("wrong password %d on %s %s = %d %s, want 401 invalid_credentials", i, g.method, g.path, r.code, r.body)
		}
	}
	right := fmt.Sprintf(`{"password":%q}`, testPassword)
	if r := fromClient(t, h, "POST", "/v1/auth/mfa/totp", right, "203.0.113.200:1234", session...); r.code != http.StatusTooManyRequests || r.json["code"] != "too_many_attempts" {
		t.Errorf("right password after the limit = %d %s, want 429 too_many_attempts, not a confirmation", r.code, r.body)
	}
}
