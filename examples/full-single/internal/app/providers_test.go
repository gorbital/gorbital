package app_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// TestSignInMethodsThroughOps checks GET /ops/auth/providers (ADR-0045):
// each method's status and what's missing, never values.
func TestSignInMethodsThroughOps(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")

	r := do(t, h, "GET", "/ops/auth/providers", "", viewer...)
	list, _ := r.json["methods"].([]any)
	if r.code != http.StatusOK || len(list) < 5 {
		t.Fatalf("GET /ops/auth/providers = %d %s", r.code, r.body)
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
	if key := strings.SplitN(testEncryptionKeys, ":", 2)[1]; strings.Contains(r.body, key) {
		t.Error("GET /ops/auth/providers returned the encryption key")
	}

	user, _ := signIn(t, a, "user@example.com", "")
	if r := do(t, h, "GET", "/ops/auth/providers", "", user...); r.code != http.StatusForbidden {
		t.Errorf("GET /ops/auth/providers without an ops role = %d, want 403", r.code)
	}
}
