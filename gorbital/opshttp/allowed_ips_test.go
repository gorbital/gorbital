package opshttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOpsAllowedIPs: with OPS_ALLOWED_IPS, /ops/ answers only clients in
// the ranges, by their address after APP_TRUSTED_PROXIES, and refuses the
// others before the sign-in check (ADR-0085). Other routes are unaffected.
func TestOpsAllowedIPs(t *testing.T) {
	a := newTestApp(t, testAppOptions{Env: map[string]string{
		"OPS_ALLOWED_IPS":     "10.0.0.0/8, 2001:db8::/32",
		"APP_TRUSTED_PROXIES": "192.0.2.100/32",
	}})
	admin, _ := a.SignIn(t, "admin@example.com", "platform_admin")

	send := func(path, remote string, headers ...string) (int, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = remote
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, req)
		var p struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		return rec.Code, p.Code
	}

	for _, tt := range []struct {
		name, path, remote string
		headers            []string
		status             int
		code               string
	}{
		{"allowed address", "/ops/settings", "10.1.2.3:4000", admin, http.StatusOK, ""},
		{"allowed IPv6 address", "/ops/system", "[2001:db8::7]:4000", admin, http.StatusOK, ""},
		{"other address", "/ops/settings", "198.51.100.7:4000", admin, http.StatusForbidden, "ip_not_allowed"},
		{"other address, signed out", "/ops/settings", "198.51.100.7:4000", nil, http.StatusForbidden, "ip_not_allowed"},
		{"allowed address, signed out", "/ops/settings", "10.1.2.3:4000", nil, http.StatusUnauthorized, "unauthenticated"},
		{"allowed client behind the trusted proxy", "/ops/settings", "192.0.2.100:4000", append([]string{"X-Forwarded-For", "10.9.9.9"}, admin...), http.StatusOK, ""},
		{"the proxy itself", "/ops/settings", "192.0.2.100:4000", admin, http.StatusForbidden, "ip_not_allowed"},
		{"an untrusted client claiming an allowed address", "/ops/settings", "198.51.100.7:4000", append([]string{"X-Forwarded-For", "10.9.9.9"}, admin...), http.StatusForbidden, "ip_not_allowed"},
		{"a route outside /ops/", "/v1/ping", "198.51.100.7:4000", nil, http.StatusOK, ""},
		{"client flags", "/v1/flags", "198.51.100.7:4000", admin, http.StatusOK, ""},
	} {
		if status, code := send(tt.path, tt.remote, tt.headers...); status != tt.status || code != tt.code {
			t.Errorf("%s: GET %s = %d %q, want %d %q", tt.name, tt.path, status, code, tt.status, tt.code)
		}
	}

	// Without OPS_ALLOWED_IPS every address reaches /ops/.
	open := newTestApp(t, testAppOptions{})
	viewer, _ := open.SignIn(t, "viewer@example.com", "ops_viewer")
	req := httptest.NewRequest(http.MethodGet, "/ops/settings", nil)
	req.RemoteAddr = "198.51.100.7:4000"
	req.Header.Set(viewer[0], viewer[1])
	rec := httptest.NewRecorder()
	open.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /ops/settings without OPS_ALLOWED_IPS = %d %s, want 200", rec.Code, rec.Body)
	}
}
