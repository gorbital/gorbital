package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/gorbitaltest"
)

// TestOpsAllowedIPsCoversEveryOpsRoute: OPS_ALLOWED_IPS is a property of the
// app, not of one module, so every route under /ops/ must refuse a client
// outside the ranges -- including the ones sign-in registers
// (/ops/auth/users, /ops/service-accounts), which are the strongest
// operations the console has: ban, grant a role, revoke sessions,
// impersonate, mint an API key.
//
// opshttp installed the filter as a group on its own module router, so it
// reached its own 50-odd routes and nothing else. An operator who set
// OPS_ALLOWED_IPS to their office range got 403 on /ops/settings and 200 on
// POST /ops/auth/users/{id}/roles from anywhere (internal security review,
// 2026-09, OPS-1).
func TestOpsAllowedIPsCoversEveryOpsRoute(t *testing.T) {
	auth := authhttp.New()
	a := gorbitaltest.NewWithEnv(t, map[string]string{
		"AUTH_ENCRYPTION_KEYS": encryptionKeys,
		"OPS_ALLOWED_IPS":      "10.0.0.0/8",
	}, options(auth)...)

	paths := opsPaths(t, a)
	if len(paths) < 60 {
		t.Fatalf("found %d /ops/ operations in the document, want every module's", len(paths))
	}
	var reached []string
	for _, op := range paths {
		req := httptest.NewRequest(op.method, op.url(), strings.NewReader("{}"))
		req.RemoteAddr = "198.51.100.7:4000" // outside the range, and not a proxy
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		a.App().Handler().ServeHTTP(rec, req)
		var p struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		if rec.Code != http.StatusForbidden || p.Code != "ip_not_allowed" {
			reached = append(reached, op.method+" "+op.path+" -> "+http.StatusText(rec.Code)+" "+p.Code)
		}
	}
	if len(reached) > 0 {
		sort.Strings(reached)
		t.Errorf("%d /ops/ operations answered a client outside OPS_ALLOWED_IPS:\n%s", len(reached), strings.Join(reached, "\n"))
	}
}

type opsOp struct{ method, path string }

// url fills the path parameters with values no row matches: the filter must
// refuse before anything looks them up.
func (o opsOp) url() string {
	return strings.NewReplacer(
		"{key}", "auth.session_idle", "{name}", "example", "{id}", "usr_x", "{role}", "ops_viewer",
		"{sessionId}", "ses_x", "{passkeyId}", "pk_x", "{identityId}", "idn_x", "{keyId}", "key_x",
		"{address}", "nobody@example.com", "{version}", "1", "{path}", "x",
	).Replace(o.path)
}

// opsPaths are the /ops/ operations of the app's OpenAPI document.
func opsPaths(t *testing.T, a *gorbitaltest.App) []opsOp {
	t.Helper()
	rec := httptest.NewRecorder()
	a.App().Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d", rec.Code)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	var ops []opsOp
	for path, item := range doc.Paths {
		if !strings.HasPrefix(path, "/ops/") {
			continue
		}
		for method := range item {
			switch strings.ToUpper(method) {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				ops = append(ops, opsOp{method: strings.ToUpper(method), path: path})
			}
		}
	}
	return ops
}
