package authhttp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey/passkeytest"
	"gorbital.dev/modules/auth/social/socialtest"
)

const (
	testConsoleToken = "signin-test-console-token-0123456789abcdef"
	testResultURL    = "http://127.0.0.1:3100/auth/test-result/"
)

// devRequest sends a request as the Dev Portal's proxy does: from loopback,
// to the app's own address, with the console token unless token is false.
// headers are name/value pairs.
func devRequest(t *testing.T, h http.Handler, method, path, body string, token bool, headers ...string) response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host, req.RemoteAddr = "127.0.0.1:8080", "127.0.0.1:52100"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token {
		req.Header.Set("Authorization", "Bearer "+testConsoleToken)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	r := response{code: rec.Code, header: rec.Header(), body: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &r.json)
	return r
}

// rowCounts counts the rows of every table sign-in writes, and the audit
// log.
func rowCounts(t *testing.T, pool *pgxpool.Pool) map[string]int {
	t.Helper()
	counts := map[string]int{}
	rows, err := pool.Query(context.Background(), `SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND (table_name LIKE 'auth\_%' OR table_name = 'audit_events')`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		tables = append(tables, name)
	}
	rows.Close()
	if len(tables) < 10 {
		t.Fatalf("found only tables %v", tables)
	}
	for _, name := range tables {
		var n int
		if err := pool.QueryRow(context.Background(), fmt.Sprintf(`SELECT count(*) FROM %q`, name)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		counts[name] = n
	}
	return counts
}

func signInTestEnv(t *testing.T, extra map[string]string) map[string]string {
	env := socialEnv(t)
	env["GITHUB_CLIENT_ID"], env["GITHUB_CLIENT_SECRET"] = "Ov23liAbCdEfGhIjKlMn", "0123456789abcdef0123456789abcdef01234567"
	env["DEV_CONSOLE_TOKEN"] = testConsoleToken
	maps.Copy(env, extra)
	return env
}

func resultOf(t *testing.T, h http.Handler, id string) map[string]any {
	t.Helper()
	r := devRequest(t, h, "GET", "/_dev/auth/test/results/"+id, "", true)
	if r.code != http.StatusOK {
		t.Fatalf("GET result %s = %d %s", id, r.code, r.body)
	}
	return r.json
}

// TestSignInTestsCreateNothing runs every live test through the app: Google,
// Apple's ID token, GitHub, a passkey ceremony and an authenticator app
// code, and checks no account, identity, session, state, nonce, passkey or
// audit event was written, no session cookie set, and results carry no
// token.
func TestSignInTestsCreateNothing(t *testing.T) {
	srv := socialtest.New(t)
	a, dbURL := newAppWithURL(t, signInTestEnv(t, nil), func(_ *gorbital.Config, a *Authenticator) {
		a.endpoints.Google, a.endpoints.Apple, a.endpoints.GitHub = srv.Endpoints(), srv.Endpoints(), srv.GitHubEndpoints()
	})
	h := a.Handler()
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	before := rowCounts(t, pool)

	overview := devRequest(t, h, "GET", "/_dev/auth/test", "", true)
	if methods, _ := overview.json["methods"].([]any); overview.code != http.StatusOK || len(methods) != 6 || strings.Contains(overview.body, "PRIVATE KEY") || strings.Contains(overview.body, "0123456789abcdef0123456789abcdef01234567") {
		t.Fatalf("GET /_dev/auth/test = %d %s", overview.code, overview.body)
	}
	if index := devRequest(t, h, "GET", "/_dev/", "", true); !strings.Contains(index.body, `"/_dev/auth/test/"`) {
		t.Errorf("the console's index doesn't list the tests: %s", index.body)
	}

	// Google: the real callback, with a real exchange.
	start := devRequest(t, h, "POST", "/_dev/auth/test/google/start", fmt.Sprintf(`{"result_url":%q}`, testResultURL), true)
	if start.code != http.StatusCreated {
		t.Fatalf("start google = %d %s", start.code, start.body)
	}
	id := start.json["id"].(string)
	provider, _ := url.Parse(start.json["url"].(string))
	q := provider.Query()
	if q.Get("redirect_uri") != "http://localhost:8080/v1/auth/google/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	code := srv.Code(socialtest.Claims{Subject: "g-test", Audience: "web-client", Email: "tester@gmail.com", EmailVerified: true, Nonce: q.Get("nonce")}, "")
	callback := do(t, h, "GET", "/v1/auth/google/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), "")
	if callback.code != http.StatusSeeOther || callback.header.Get("Location") != testResultURL+"#id="+id+"&method=google" || len(callback.header.Values("Set-Cookie")) != 0 {
		t.Fatalf("callback = %d %q %v %s", callback.code, callback.header.Get("Location"), callback.header.Values("Set-Cookie"), callback.body)
	}
	if r := resultOf(t, h, id); r["state"] != "passed" || r["identity"].(map[string]any)["email"] != "tester@gmail.com" {
		t.Errorf("google result = %v", r)
	}
	// The same return again stays with the test.
	if again := do(t, h, "GET", "/v1/auth/google/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), ""); again.code != http.StatusSeeOther || !strings.HasPrefix(again.header.Get("Location"), testResultURL) {
		t.Errorf("replayed callback = %d %q", again.code, again.header.Get("Location"))
	}

	// GitHub.
	start = devRequest(t, h, "POST", "/_dev/auth/test/github/start", fmt.Sprintf(`{"result_url":%q}`, testResultURL), true)
	provider, _ = url.Parse(start.json["url"].(string))
	q = provider.Query()
	code = srv.GitHubCode(socialtest.GitHubUser{ID: 7, Login: "octo", Emails: []socialtest.GitHubEmail{{Email: "octo@example.com", Primary: true, Verified: true}}}, q.Get("code_challenge"))
	if cb := do(t, h, "GET", "/v1/auth/github/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), ""); cb.code != http.StatusSeeOther {
		t.Fatalf("github callback = %d %s", cb.code, cb.body)
	}
	if r := resultOf(t, h, start.json["id"].(string)); r["state"] != "passed" {
		t.Errorf("github result = %v", r)
	}

	// Apple's native ID token (the web flow needs https).
	token := srv.IDToken(socialtest.Claims{Subject: "001.x", Audience: "com.example.app", Email: "x@icloud.com", EmailVerified: true, Nonce: hashedNonce("raw-nonce")})
	verified := devRequest(t, h, "POST", "/_dev/auth/test/apple/id-token", fmt.Sprintf(`{"id_token":%q,"nonce":"raw-nonce"}`, token), true)
	if verified.code != http.StatusOK || verified.json["state"] != "passed" || strings.Contains(verified.body, token) {
		t.Errorf("apple id-token = %d %s", verified.code, verified.body)
	}
	if r := devRequest(t, h, "POST", "/_dev/auth/test/apple/start", fmt.Sprintf(`{"result_url":%q}`, testResultURL), true); r.code != http.StatusConflict || r.json["code"] != "live_test_unavailable" {
		t.Errorf("apple start on http = %d %s", r.code, r.body)
	}

	// A passkey ceremony on the app's origin.
	start = devRequest(t, h, "POST", "/_dev/auth/test/passkeys/start", fmt.Sprintf(`{"result_url":%q}`, testResultURL), true)
	if start.code != http.StatusCreated {
		t.Fatalf("start passkeys = %d %s", start.code, start.body)
	}
	_, ticket, _ := strings.Cut(start.json["url"].(string), "#ticket=")
	if page := do(t, h, "GET", "/_signin-test/passkey", ""); page.code != http.StatusOK || !strings.Contains(page.header.Get("Content-Security-Policy"), "script-src 'nonce-") {
		t.Errorf("ceremony page = %d %v", page.code, page.header)
	}
	authenticator := passkeytest.New("http://localhost:8080")
	options := do(t, h, "POST", "/_signin-test/passkey/options", fmt.Sprintf(`{"ticket":%q,"step":"register"}`, ticket))
	optionsJSON, _ := json.Marshal(options.json["options"])
	created, err := authenticator.Create(optionsJSON)
	if err != nil {
		t.Fatalf("options = %d %s: %v", options.code, options.body, err)
	}
	if r := do(t, h, "POST", "/_signin-test/passkey/finish", fmt.Sprintf(`{"ticket":%q,"step":"register","response":%s}`, ticket, created)); r.json["next"] != "login" {
		t.Fatalf("register = %d %s", r.code, r.body)
	}
	options = do(t, h, "POST", "/_signin-test/passkey/options", fmt.Sprintf(`{"ticket":%q,"step":"login"}`, ticket))
	optionsJSON, _ = json.Marshal(options.json["options"])
	assertion, err := authenticator.Get(optionsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if r := do(t, h, "POST", "/_signin-test/passkey/finish", fmt.Sprintf(`{"ticket":%q,"step":"login","response":%s}`, ticket, assertion)); r.json["next"] != "done" {
		t.Fatalf("login = %d %s", r.code, r.body)
	}
	if r := resultOf(t, h, start.json["id"].(string)); r["state"] != "passed" {
		t.Errorf("passkeys result = %v", r)
	}

	// An authenticator app.
	totp := devRequest(t, h, "POST", "/_dev/auth/test/totp/start", "", true)
	current, _ := authlib.TOTPCode(totp.json["secret"].(string), timeNow())
	if r := devRequest(t, h, "POST", "/_dev/auth/test/totp/verify", fmt.Sprintf(`{"id":%q,"code":%q}`, totp.json["id"], current), true); r.json["passed"] != true {
		t.Errorf("totp verify = %d %s", r.code, r.body)
	}

	if after := rowCounts(t, pool); !maps.Equal(before, after) {
		t.Errorf("the tests wrote to the database:\nbefore %v\nafter  %v", before, after)
	}
}

// TestSignInTestsRefusals: without the console token, through a tunnel, on
// a public Host, or with the console off, nothing about the tests answers;
// a forged or sign-in state isn't a test's; and sign-in itself still works
// with the tests on.
func TestSignInTestsRefusals(t *testing.T) {
	srv := socialtest.New(t)
	h := newApp(t, signInTestEnv(t, nil), func(_ *gorbital.Config, a *Authenticator) {
		a.endpoints.Google, a.endpoints.Apple, a.endpoints.GitHub = srv.Endpoints(), srv.Endpoints(), srv.GitHubEndpoints()
	}).Handler()
	start := `{"result_url":"` + testResultURL + `"}`
	for name, r := range map[string]response{
		"no token":    devRequest(t, h, "POST", "/_dev/auth/test/google/start", start, false),
		"wrong token": devRequest(t, h, "POST", "/_dev/auth/test/google/start", start, false, "Authorization", "Bearer "+strings.Repeat("x", 40)),
		"tunnelled":   devRequest(t, h, "POST", "/_dev/auth/test/google/start", start, true, "Cf-Connecting-Ip", "203.0.113.9", "Cf-Ray", "8f"),
		"forwarded":   devRequest(t, h, "GET", "/_dev/auth/test/results/x", "", true, "X-Forwarded-For", "203.0.113.9"),
		"public host": do(t, h, "POST", "/_dev/auth/test/google/start", start, "Authorization", "Bearer "+testConsoleToken),
	} {
		if r.code != http.StatusUnauthorized && r.code != http.StatusForbidden {
			t.Errorf("%s: %d %s, want refused", name, r.code, r.body)
		}
	}
	tunnelHost := httptest.NewRequest("GET", "/_dev/auth/test", nil)
	tunnelHost.Host, tunnelHost.RemoteAddr = "dev.example.com", "127.0.0.1:5000"
	tunnelHost.Header.Set("Authorization", "Bearer "+testConsoleToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, tunnelHost)
	if rec.Code != http.StatusForbidden {
		t.Errorf("tunnel hostname from loopback = %d, want 403", rec.Code)
	}

	// Sign-in works with the tests on, and its state isn't a test's.
	signIn := do(t, h, "GET", "/v1/auth/google/start", "")
	location, _ := url.Parse(signIn.header.Get("Location"))
	q := location.Query()
	code := srv.Code(socialtest.Claims{Subject: "g-real", Audience: "web-client", Email: "real@gmail.com", EmailVerified: true, Nonce: q.Get("nonce")}, "")
	cb := do(t, h, "GET", "/v1/auth/google/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), "", "Cookie", "__Host-oauth="+cookieValue(signIn, "__Host-oauth"))
	if cb.code != http.StatusSeeOther || cookieValue(cb, "__Host-session") == "" || strings.HasPrefix(cb.header.Get("Location"), testResultURL) {
		t.Errorf("sign-in with the tests on = %d %q %v", cb.code, cb.header.Get("Location"), cb.header.Values("Set-Cookie"))
	}
	if forged := do(t, h, "GET", "/v1/auth/google/callback?code=x&state=forged-test-state", ""); forged.code != http.StatusSeeOther || !strings.HasSuffix(forged.header.Get("Location"), "#error=invalid_state") {
		t.Errorf("a forged state = %d %q, want sign-in's invalid_state", forged.code, forged.header.Get("Location"))
	}

	// With the console off there are no tests: the state of one never
	// existed, and the endpoints and the ceremony page aren't served.
	off := newApp(t, signInTestEnv(t, map[string]string{"DEV_CONSOLE_TOKEN": ""})).Handler()
	if r := devRequest(t, off, "GET", "/_dev/auth/test", "", true); r.code != http.StatusNotFound {
		t.Errorf("GET /_dev/auth/test without the console = %d", r.code)
	}
	if r := do(t, off, "GET", "/_signin-test/passkey", ""); r.code != http.StatusNotFound {
		t.Errorf("ceremony page without the console = %d", r.code)
	}
}

// TestSignInTestsGate pins the gate the tests hang on (ADR-0087): an app
// built with the dev console off serves none of their surface and runs none
// of their code. The same app with the console on is the control, so a
// refactor that takes the feature away can't leave this test passing.
func TestSignInTestsGate(t *testing.T) {
	srv := socialtest.New(t)
	endpoints := func(_ *gorbital.Config, a *Authenticator) {
		a.endpoints.Google, a.endpoints.Apple, a.endpoints.GitHub = srv.Endpoints(), srv.Endpoints(), srv.GitHubEndpoints()
	}
	offApp := newApp(t, signInTestEnv(t, map[string]string{"DEV_CONSOLE_TOKEN": ""}), endpoints)
	onApp := newApp(t, signInTestEnv(t, nil), endpoints)
	off, on := offApp.Handler(), onApp.Handler()

	// The console's endpoints: the overview under both spellings, and a
	// deeper path that a console mounted but empty would answer 404 itself.
	// These are held by two gates -- mountSignInTests and the AuthSetup's
	// own DevEndpoints -- so they stay 404 even if the first one goes.
	start := `{"result_url":"` + testResultURL + `"}`
	for _, c := range []struct {
		method, path, body string
		on                 int // what the console serves when it is on
	}{
		{"GET", "/_dev/auth/test", "", http.StatusOK},
		{"GET", "/_dev/auth/test/", "", http.StatusOK},
		{"POST", "/_dev/auth/test/google/start", start, http.StatusCreated},
	} {
		if r := devRequest(t, off, c.method, c.path, c.body, true); r.code != http.StatusNotFound {
			t.Errorf("%s %s with the console off = %d %s, want 404", c.method, c.path, r.code, r.body)
		}
		if r := devRequest(t, on, c.method, c.path, c.body, true); r.code != c.on {
			t.Errorf("%s %s with the console on = %d %s, want %d", c.method, c.path, r.code, r.body, c.on)
		}
		if r := devRequest(t, on, c.method, c.path, c.body, false); r.code != http.StatusUnauthorized {
			t.Errorf("%s %s with the console on and no token = %d %s, want 401", c.method, c.path, r.code, r.body)
		}
	}

	// The ceremony page, which the console's checks never see: it is there
	// with the console on and nowhere with it off.
	if r := do(t, off, "GET", "/_signin-test/passkey", ""); r.code != http.StatusNotFound {
		t.Errorf("the ceremony page with the console off = %d, want 404", r.code)
	}
	if r := do(t, on, "GET", "/_signin-test/passkey", ""); r.code != http.StatusOK {
		t.Errorf("the ceremony page with the console on = %d %s, want 200", r.code, r.body)
	}

	// The one route outside /_dev/: the interceptor isn't on sign-in's
	// callbacks at all, so a provider's return is sign-in's own. An unknown
	// state is sign-in's invalid_state, never a test's result page.
	if tester := offApp.auth.signInTester(); tester != nil {
		t.Error("the callback interceptor is installed with the console off")
	}
	if tester := onApp.auth.signInTester(); tester == nil {
		t.Error("the callback interceptor is missing with the console on")
	}
	cb := do(t, off, "GET", "/v1/auth/google/callback?state=signin-test-state&code=signin-test-code", "")
	if cb.code != http.StatusSeeOther || !strings.HasSuffix(cb.header.Get("Location"), "#error=invalid_state") {
		t.Errorf("a callback with the console off = %d %q, want sign-in's invalid_state", cb.code, cb.header.Get("Location"))
	}
	// And a real round trip on the same callback signs in.
	signIn := do(t, off, "GET", "/v1/auth/google/start", "")
	location, _ := url.Parse(signIn.header.Get("Location"))
	q := location.Query()
	code := srv.Code(socialtest.Claims{Subject: "g-gate", Audience: "web-client", Email: "gate@gmail.com", EmailVerified: true, Nonce: q.Get("nonce")}, "")
	cb = do(t, off, "GET", "/v1/auth/google/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(q.Get("state")), "", "Cookie", "__Host-oauth="+cookieValue(signIn, "__Host-oauth"))
	if cb.code != http.StatusSeeOther || cookieValue(cb, "__Host-session") == "" || strings.Contains(cb.header.Get("Location"), "#error=") || strings.HasPrefix(cb.header.Get("Location"), testResultURL) {
		t.Errorf("sign-in with the console off = %d %q %v", cb.code, cb.header.Get("Location"), cb.header.Values("Set-Cookie"))
	}
}

// TestSignInTestsLeaveTheOpenAPIDocument: the interception adds no
// operation, parameter or response to sign-in's document.
func TestSignInTestsLeaveTheOpenAPIDocument(t *testing.T) {
	on := do(t, newApp(t, signInTestEnv(t, nil)).Handler(), "GET", "/openapi.json", "")
	off := do(t, newApp(t, signInTestEnv(t, map[string]string{"DEV_CONSOLE_TOKEN": ""})).Handler(), "GET", "/openapi.json", "")
	if on.code != http.StatusOK || on.body != off.body {
		t.Errorf("the OpenAPI document changes with the dev console on (%d, %d bytes vs %d)", on.code, len(on.body), len(off.body))
	}
}

func hashedNonce(nonce string) string {
	sum := sha256.Sum256([]byte(nonce))
	return hex.EncodeToString(sum[:])
}

func timeNow() time.Time { return time.Now() }
