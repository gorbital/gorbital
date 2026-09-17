package opshttp_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
)

// This test is TestOpsRateLimits from a v0.1 golden app's
// internal/app/ops_auth_test.go, run against the library module. The rest
// of that file operates user accounts, which authhttp serves; with sign-in's
// own limiters, it runs in gorbital/internal/integration. devServer and
// devDo are in devoperator_test.go.

func TestOpsRateLimits(t *testing.T) {
	// Listing limiters needs ops.auth.read and resetting them ops.auth.write,
	// which sign-in (authhttp) declares for platform_admin, and so for the
	// dev console's operator. A stand-in module declares them as authhttp
	// does. devServer can't add modules, so the server is built here.
	signIn := gorbital.Module{Name: "signin", Permissions: []gorbital.Permission{
		{Name: "ops.auth.read", Description: "See which sign-in methods are configured", Roles: []string{"platform_admin", "ops_viewer"}},
		{Name: "ops.auth.write", Description: "Manage accounts and reset rate limits", Roles: []string{"platform_admin"}},
	}}
	env := map[string]string{"DEV_CONSOLE_TOKEN": devToken}
	a := newTestApp(t, testAppOptions{Env: env, Gorbital: []gorbital.Option{gorbital.WithModules(signIn)}})
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)
	base := srv.URL
	code, body := devDo(t, base, http.MethodGet, "/ops/auth/rate-limits", "")
	// The golden app listed sign-in's auth_login limiter, which authhttp
	// declares; without it, an app has the built-in auth_ip limiter and the
	// ops module's ops_test_email.
	if code != http.StatusOK || !strings.Contains(body, `"name":"auth_ip"`) || !strings.Contains(body, `"name":"ops_test_email"`) || !strings.Contains(body, `"keys":`) {
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
