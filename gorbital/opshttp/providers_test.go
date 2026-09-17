package opshttp_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/gorbital"
)

// This test is a v0.1 golden app's internal/app/providers_test.go, run
// against the library module with a test authenticator that reports the
// methods, as the golden app reported them in development without provider
// configuration. gorbital/internal/integration runs it with authhttp's own
// report.

// TestSignInMethodsThroughOps checks GET /ops/auth/providers (ADR-0045):
// each method's status and what's missing, never values.
func TestSignInMethodsThroughOps(t *testing.T) {
	report := []gorbital.SignInMethod{
		{Key: "password", Name: "Email and password", Enabled: true},
		{Key: "passkeys", Name: "Passkeys in browsers", Enabled: true, Detail: "RP ID localhost, origins http://localhost:8080"},
		{Key: "passkeys_ios", Name: "Passkeys in iOS apps", Missing: []string{"WEBAUTHN_APPLE_APP_IDS"}, Guide: "AUTH_PROVIDERS.md#passkeys-in-ios-apps"},
		{Key: "github", Name: "GitHub", Missing: []string{"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET"}, Guide: "AUTH_PROVIDERS.md#github-sign-in"},
		{Key: "authenticator_app", Name: "Authenticator app", Enabled: true},
	}
	a := newTestApp(t, testAppOptions{SignInMethods: report})
	h := a.Handler()
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")

	r := do(t, h, "GET", "/ops/auth/providers", "", viewer...)
	list, _ := r.JSON["methods"].([]any)
	if r.Code != http.StatusOK || len(list) < 5 {
		t.Fatalf("GET /ops/auth/providers = %d %s", r.Code, r.Body)
	}
	methods := map[string]map[string]any{}
	for _, m := range list {
		method := m.(map[string]any)
		methods[method["key"].(string)] = method
	}
	if m := methods["passkeys"]; m["enabled"] != true || !strings.Contains(m["detail"].(string), "RP ID localhost") || m["missing"] != nil {
		t.Errorf("passkeys = %v, want enabled on localhost", m)
	}
	if m := methods["passkeys_ios"]; m["enabled"] != false || !slices.Contains(m["missing"].([]any), any("WEBAUTHN_APPLE_APP_IDS")) ||
		m["guide"] != "AUTH_PROVIDERS.md#passkeys-in-ios-apps" {
		t.Errorf("passkeys_ios = %v, want disabled with what to set", m)
	}
	if m := methods["github"]; m["enabled"] != false || !slices.Equal(m["missing"].([]any), []any{"GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET"}) ||
		m["guide"] != "AUTH_PROVIDERS.md#github-sign-in" {
		t.Errorf("github = %v, want disabled with what to set", m)
	}
	if m := methods["authenticator_app"]; m["enabled"] != true {
		t.Errorf("authenticator_app = %v, want enabled with encryption keys", m)
	}
	// The golden test also checked that the response doesn't hold
	// AUTH_ENCRYPTION_KEYS: the module only reports what the authenticator
	// returns, so authhttp's tests check that.

	user, _ := a.SignIn(t, "user@example.com", "")
	if r := do(t, h, "GET", "/ops/auth/providers", "", user...); r.Code != http.StatusForbidden {
		t.Errorf("GET /ops/auth/providers without an ops role = %d, want 403", r.Code)
	}

	// Not in the golden test: an authenticator that doesn't report its
	// methods gets an empty list.
	b := newTestApp(t, testAppOptions{})
	viewer, _ = b.SignIn(t, "viewer@example.com", "ops_viewer")
	if r := do(t, b.Handler(), "GET", "/ops/auth/providers", "", viewer...); r.Code != http.StatusOK || !strings.Contains(r.Body, `"methods":[]`) {
		t.Errorf("GET /ops/auth/providers without a report = %d %s, want an empty list", r.Code, r.Body)
	}
}
