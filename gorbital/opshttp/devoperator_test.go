package opshttp_test

import (
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorbital.dev/gorbital/internal/opstest"
	authlib "gorbital.dev/modules/auth"
)

// This test and its helpers are from a v0.1 golden app's
// internal/app/devconsole_test.go (devDo from ops_auth_test.go), run against
// the library module. The ops_storage and rate-limit tests use the helpers
// too.

// devToken is a dev console token as orb dev generates them: 256 bits,
// base64url.
const devToken = "q3Jt0tBq0Xvqf7i5Tq1hYw2m9x8Zr4Kc6Lp2Nd5Vb3E"

// devServer serves an app with the dev console on over a real loopback
// listener and returns the app and its URL.
func devServer(t *testing.T, env map[string]string) (*opstest.App, string) {
	t.Helper()
	full := map[string]string{"DEV_CONSOLE_TOKEN": devToken}
	maps.Copy(full, env)
	a := opstest.New(t, opstest.Options{Env: full})
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
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

func TestDevOperator(t *testing.T) {
	_, base := devServer(t, nil)
	port := base[strings.LastIndex(base, ":")+1:]

	// The console token operates /ops/ in development, as a system actor.
	code, _, body := devGet(t, base, "/ops/settings")
	if code != http.StatusOK || !strings.Contains(body, `"settings":[`) {
		t.Fatalf("GET /ops/settings with the console token = %d %s, want 200", code, body)
	}
	req, _ := http.NewRequest(http.MethodPut, base+"/ops/settings/example.ping_message", strings.NewReader(`{"value":"from the portal","version":0,"reason":"dev portal test"}`))
	req.Header.Set("Authorization", "Bearer "+devToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("PUT /ops/settings as the operator = %d, want 200", res.StatusCode)
	}
	if code, _, body := devGet(t, base, "/ops/audit?actor_kind=system"); code != http.StatusOK || !strings.Contains(body, `"actor_id":"dev-console"`) {
		t.Errorf("the change wasn't audited as system/dev-console: %d %s", code, body)
	}

	// Never outside /ops/, never with the wrong Host, never in a cookie.
	// The golden test called GET /v1/auth/me, which comes with sign-in in
	// Phase 5; GET /v1/flags also needs a signed-in user.
	if code, _, body := devGet(t, base, "/v1/flags"); code != http.StatusUnauthorized {
		t.Errorf("GET /v1/flags with the console token = %d %s, want 401", code, body)
	}
	for _, host := range []string{"evil.example:" + port, "localhost:1"} {
		if code, _, _ := devGet(t, base, "/ops/settings", "Host", host); code != http.StatusUnauthorized {
			t.Errorf("Host %s: GET /ops/settings = %d, want 401 (the operator needs a localhost Host)", host, code)
		}
	}
	if code, _, _ := devGet(t, base, "/ops/settings", "Authorization", "", "Cookie", authlib.DefaultCookieName+"="+devToken); code != http.StatusUnauthorized {
		t.Errorf("token in the session cookie = %d, want 401", code)
	}
	if code, _, _ := devGet(t, base, "/ops/settings", "Authorization", "Bearer "+devToken[1:]); code != http.StatusUnauthorized {
		t.Errorf("a wrong token = %d, want 401", code)
	}

	// Without the console there is no operator: the token is just an unknown one.
	srv := httptest.NewServer(opstest.New(t, opstest.Options{}).Handler())
	defer srv.Close()
	if code, _, _ := devGet(t, srv.URL, "/ops/settings"); code != http.StatusUnauthorized {
		t.Errorf("GET /ops/settings with the token but no console = %d, want 401", code)
	}
}
