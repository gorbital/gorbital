package authhttp

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authlib "gorbital.dev/modules/auth"
)

// publicEndpoint is an endpoint anyone may call; a valid API key on it is
// accepted. The golden app used its GET /v1/ping.
const publicEndpoint = "/version"

// createKey creates an API key with a POST to path and returns the key and
// its ID.
func createKey(t *testing.T, h http.Handler, path, body string, headers []string) (key, id string) {
	t.Helper()
	r := do(t, h, "POST", path, body, headers...)
	key, _ = r.json["key"].(string)
	apiKey, _ := r.json["api_key"].(map[string]any)
	id, _ = apiKey["id"].(string)
	if r.code != http.StatusCreated || !strings.HasPrefix(key, authlib.APIKeyPrefix) || id == "" {
		t.Fatalf("POST %s = %d %s, want 201 with a key", path, r.code, r.body)
	}
	if r.header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(key, apiKey["prefix"].(string)+"_") {
		t.Errorf("POST %s: Cache-Control %q, prefix %v; want no-store and the key's start", path, r.header.Get("Cache-Control"), apiKey["prefix"])
	}
	return key, id
}

// withIdempotencyKey returns headers with an Idempotency-Key.
func withIdempotencyKey(headers []string, key string) []string {
	return append(slices.Clone(headers), "Idempotency-Key", key)
}

// noSecretsStored fails when a key's secret appears in audit events or queued
// jobs.
func noSecretsStored(t *testing.T, dbURL string, keys ...string) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var text string
	err = pool.QueryRow(context.Background(), `
		SELECT COALESCE((SELECT string_agg(e::text, ' ') FROM audit_events e), '') || COALESCE((SELECT string_agg(j.args::text, ' ') FROM river_job j), '')`).Scan(&text)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "auth.api_key.created") {
		t.Fatalf("audit events have no auth.api_key.created: %.300s", text)
	}
	for _, k := range keys {
		if strings.Contains(text, k[len(authlib.APIKeyPrefix)+27:]) {
			t.Errorf("an API key's secret is stored in audit events or jobs")
		}
	}
}

// TestAPIKeysEndToEnd follows ADR-0058 over HTTP: create a key with the
// password, use it as a bearer token, keep it away from account management
// and /ops, revoke it, and limit guessing.
func TestAPIKeysEndToEnd(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	ada, _ := signIn(t, a, "ada@example.com", "")
	expires := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	body := func(extra string) string { return fmt.Sprintf(`{"name":"CI","expires_at":%q%s}`, expires, extra) }

	if r := do(t, h, "POST", "/v1/auth/api-keys", body("")); r.code != http.StatusUnauthorized {
		t.Errorf("create without signing in = %d %s, want 401", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/api-keys", body(`,"password":"wrong password here"`), ada...); r.code != http.StatusUnauthorized || r.json["code"] != "invalid_credentials" {
		t.Errorf("create with a wrong password = %d %s, want 401 invalid_credentials", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/api-keys", fmt.Sprintf(`{"name":"CI","password":%q}`, testPassword), ada...); r.code != http.StatusUnprocessableEntity {
		t.Errorf("create without an expiry = %d %s, want 422", r.code, r.body)
	}
	far := time.Now().Add(91 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if r := do(t, h, "POST", "/v1/auth/api-keys", fmt.Sprintf(`{"name":"CI","expires_at":%q,"password":%q}`, far, testPassword), ada...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "invalid_api_key_expiry" {
		t.Errorf("create beyond auth.api_key_max_ttl = %d %s, want 422 invalid_api_key_expiry", r.code, r.body)
	}
	// A declared permission the user lacks (the golden app used
	// ops.settings.read, the operations module's).
	if r := do(t, h, "POST", "/v1/auth/api-keys", body(fmt.Sprintf(`,"password":%q,"scopes":["ops.auth.read"]`, testPassword)), ada...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "invalid_api_key_scopes" {
		t.Errorf("create with a scope the user lacks = %d %s, want 422 invalid_api_key_scopes", r.code, r.body)
	}
	key, id := createKey(t, h, "/v1/auth/api-keys", body(fmt.Sprintf(`,"password":%q`, testPassword)), ada)
	bearer := []string{"Authorization", "Bearer " + key}

	if r := do(t, h, "GET", signedInEndpoint, "", bearer...); r.code != http.StatusOK {
		t.Errorf("GET %s with the key = %d %s, want 200", signedInEndpoint, r.code, r.body)
	}
	// A key can't manage the account, its sessions or keys.
	for _, req := range []struct{ method, path, body string }{
		{"GET", "/v1/auth/me", ""},
		{"GET", "/v1/auth/api-keys", ""},
		{"POST", "/v1/auth/api-keys", body(fmt.Sprintf(`,"password":%q`, testPassword))},
		{"DELETE", "/v1/auth/api-keys/" + id, ""},
		{"GET", "/v1/auth/sessions", ""},
		{"POST", "/v1/auth/logout-all", ""},
		{"PUT", "/v1/auth/password", fmt.Sprintf(`{"current_password":%q,"new_password":"a brand new password"}`, testPassword)},
		{"GET", "/ops/service-accounts", ""},
	} {
		if r := do(t, h, req.method, req.path, req.body, bearer...); r.code != http.StatusForbidden || r.json["code"] != "session_required" {
			t.Errorf("%s %s with a key = %d %s, want 403 session_required", req.method, req.path, r.code, r.body)
		}
	}
	// A key is never a session: not in the session cookie, not changed.
	wrong := key[:len(key)-1] + "a"
	if wrong == key {
		wrong = key[:len(key)-1] + "b"
	}
	for name, headers := range map[string][]string{
		"key as the session cookie": {"Cookie", "__Host-session=" + key},
		"wrong secret":              {"Authorization", "Bearer " + wrong},
		"lowercase bearer":          {"Authorization", "bearer " + key},
		"uppercase key":             {"Authorization", "Bearer " + strings.ToUpper(key)},
	} {
		if r := do(t, h, "GET", signedInEndpoint, "", headers...); r.code != http.StatusUnauthorized {
			t.Errorf("%s: GET %s = %d %s, want 401", name, signedInEndpoint, r.code, r.body)
		}
	}

	list := do(t, h, "GET", "/v1/auth/api-keys", "", ada...)
	if keys, _ := list.json["api_keys"].([]any); list.code != http.StatusOK || len(keys) != 1 || strings.Contains(list.body, key[len(authlib.APIKeyPrefix)+27:]) ||
		keys[0].(map[string]any)["status"] != "active" || keys[0].(map[string]any)["last_used_at"] == nil {
		t.Errorf("GET /v1/auth/api-keys = %d %s, want the used key without its secret", list.code, list.body)
	}
	if r := do(t, h, "DELETE", "/v1/auth/api-keys/"+id, "", ada...); r.code != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", signedInEndpoint, "", bearer...); r.code != http.StatusUnauthorized {
		t.Errorf("GET %s with a revoked key = %d %s, want 401", signedInEndpoint, r.code, r.body)
	}

	// An operator's key never reaches /ops: its roles require two-factor
	// authentication. The golden app checked the operations module's
	// endpoints (/ops/settings, /ops/audit, /ops/observability/...,
	// /ops/incidents), which move with /ops in Phase 4; these are sign-in's.
	admin, _ := signIn(t, a, "admin@example.com", "platform_admin")
	adminKey, _ := createKey(t, h, "/v1/auth/api-keys", body(""), admin) // the session verified a second factor just now
	for _, path := range []string{"/ops/auth/users"} {
		if r := do(t, h, "GET", path, "", "Authorization", "Bearer "+adminKey); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
			t.Errorf("GET %s with a platform admin's key = %d %s, want 403 forbidden", path, r.code, r.body)
		}
	}
	noSecretsStored(t, dbURL, key, adminKey)

	// Wrong keys from one network are limited (auth.api_key_failures_per_minute);
	// a valid key still works.
	var last response
	for range 31 {
		last = do(t, h, "GET", signedInEndpoint, "", "Authorization", "Bearer "+wrong)
	}
	if last.code != http.StatusTooManyRequests || last.json["code"] != "too_many_attempts" || last.header.Get("Retry-After") == "" {
		t.Errorf("31st wrong key = %d %s, want 429 too_many_attempts with Retry-After", last.code, last.body)
	}
	if r := do(t, h, "GET", publicEndpoint, "", "Authorization", "Bearer "+adminKey); r.code != http.StatusOK {
		t.Errorf("valid key from the limited network = %d %s, want 200", r.code, r.body)
	}
}

// TestAPIKeyScopesCoverOwnData checks that a key's scopes limit what it does
// with the user's own data, which needs no granted role: a read-only key
// lists and reads projects but can't change them, while the session and an
// unscoped key can (ADR-0058).
func TestAPIKeyScopesCoverOwnData(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	ada, _ := signIn(t, a, "ada@example.com", "")
	projects := projectsOf(t, h, ada)
	created := do(t, h, "POST", projects, `{"name":"Website"}`, ada...)
	if created.code != http.StatusCreated {
		t.Fatalf("create with the session = %d %s", created.code, created.body)
	}
	project := projects + "/" + created.json["id"].(string)
	expires := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	key := func(scopes string) []string {
		k, _ := createKey(t, h, "/v1/auth/api-keys", fmt.Sprintf(`{"name":"CI","expires_at":%q,"password":%q,"scopes":%s}`, expires, testPassword, scopes), ada)
		return []string{"Authorization", "Bearer " + k}
	}

	reader := key(`["projects.project.read"]`)
	for _, path := range []string{projects, project} {
		if r := do(t, h, "GET", path, "", reader...); r.code != http.StatusOK {
			t.Errorf("GET %s with a read-only key = %d %s, want 200", path, r.code, r.body)
		}
	}
	for _, req := range []struct{ method, path, body string }{
		{"POST", projects, `{"name":"By a key"}`},
		{"PATCH", project, `{"version":1,"name":"By a key"}`},
		{"DELETE", project, ""},
	} {
		if r := do(t, h, req.method, req.path, req.body, reader...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
			t.Errorf("%s %s with a read-only key = %d %s, want 403 forbidden", req.method, req.path, r.code, r.body)
		}
	}
	if r := do(t, h, "GET", "/v1/flags", "", reader...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("GET /v1/flags with a key without flags.flag.read = %d %s, want 403 forbidden", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/flags", "", key(`["flags.flag.read"]`)...); r.code != http.StatusOK {
		t.Errorf("GET /v1/flags with flags.flag.read = %d %s, want 200", r.code, r.body)
	}
	writer := key(`["projects.project.write"]`)
	if r := do(t, h, "GET", projects, "", writer...); r.code != http.StatusForbidden {
		t.Errorf("list with a write-only key = %d %s, want 403", r.code, r.body)
	}

	// The session and an unscoped key keep everything the user holds.
	if r := do(t, h, "PATCH", project, `{"version":1,"name":"Website v2"}`, ada...); r.code != http.StatusOK {
		t.Errorf("update with the session = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", project, "", key(`[]`)...); r.code != http.StatusNoContent {
		t.Errorf("delete with an unscoped key = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/auth/api-keys", fmt.Sprintf(`{"name":"CI","expires_at":%q,"password":%q,"scopes":["projects.project.admin"]}`, expires, testPassword), ada...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "invalid_api_key_scopes" {
		t.Errorf("create with an undeclared scope = %d %s, want 422 invalid_api_key_scopes", r.code, r.body)
	}
}

// TestServiceAccountsThroughOps manages a platform service account over
// /ops: permissions, roles that require two-factor authentication, keys,
// disabling and deleting.
func TestServiceAccountsThroughOps(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	admin, _ := signIn(t, a, "admin@example.com", "platform_admin")
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")
	user, _ := signIn(t, a, "user@example.com", "")

	if r := do(t, h, "GET", "/ops/service-accounts", ""); r.code != http.StatusUnauthorized {
		t.Errorf("list without signing in = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", "/ops/service-accounts", "", user...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("list without a role = %d %s, want 403 forbidden", r.code, r.body)
	}
	for _, role := range []string{"platform_admin", "ops_viewer", "nope"} {
		if r := do(t, h, "POST", "/ops/service-accounts", fmt.Sprintf(`{"name":"robot","roles":[%q]}`, role), admin...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "invalid_service_account_role" {
			t.Errorf("create with role %s = %d %s, want 422 invalid_service_account_role", role, r.code, r.body)
		}
	}
	created := do(t, h, "POST", "/ops/service-accounts", `{"name":"Billing sync","description":"Nightly"}`, admin...)
	id, _ := created.json["id"].(string)
	if created.code != http.StatusCreated || !strings.HasPrefix(id, "svc_") || created.json["disabled"] != false {
		t.Fatalf("create = %d %s", created.code, created.body)
	}
	item := "/ops/service-accounts/" + id
	if r := do(t, h, "POST", "/ops/service-accounts", `{"name":"other"}`, viewer...); r.code != http.StatusForbidden {
		t.Errorf("create as ops_viewer = %d %s, want 403", r.code, r.body)
	}
	if r := do(t, h, "GET", item, "", viewer...); r.code != http.StatusOK || r.json["name"] != "Billing sync" {
		t.Errorf("get as ops_viewer = %d %s", r.code, r.body)
	}

	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	key, keyID := createKey(t, h, item+"/keys", fmt.Sprintf(`{"name":"sync","expires_at":%q}`, expires), admin)

	// A response showing a key is never kept for Idempotency-Key replay
	// (Cache-Control: no-store, ADR-0060): a retry creates another key
	// instead of returning the first one again.
	other := do(t, h, "POST", "/ops/service-accounts", `{"name":"Retried sync"}`, admin...)
	otherID, _ := other.json["id"].(string)
	retry := fmt.Sprintf(`{"name":"retried","expires_at":%q}`, expires)
	first, _ := createKey(t, h, "/ops/service-accounts/"+otherID+"/keys", retry, withIdempotencyKey(admin, "create-sync-key"))
	second, _ := createKey(t, h, "/ops/service-accounts/"+otherID+"/keys", retry, withIdempotencyKey(admin, "create-sync-key"))
	if first == second {
		t.Error("a retried key creation replayed the stored key")
	}
	bearer := []string{"Authorization", "Bearer " + key}
	// A service account is no user, has no account to manage and never
	// reaches /ops. The golden app checked /ops/settings (the operations
	// module's, Phase 4); /ops/auth/users is sign-in's.
	if r := do(t, h, "GET", signedInEndpoint, "", bearer...); r.code != http.StatusUnauthorized {
		t.Errorf("GET %s with a service account's key = %d %s, want 401", signedInEndpoint, r.code, r.body)
	}
	if r := do(t, h, "GET", "/ops/auth/users", "", bearer...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("GET /ops/auth/users with a service account's key = %d %s, want 403 forbidden", r.code, r.body)
	}
	for _, path := range []string{"/v1/auth/me", item} {
		if r := do(t, h, "GET", path, "", bearer...); r.code != http.StatusForbidden || r.json["code"] != "session_required" {
			t.Errorf("GET %s with a service account's key = %d %s, want 403 session_required", path, r.code, r.body)
		}
	}
	if r := do(t, h, "GET", publicEndpoint, "", bearer...); r.code != http.StatusOK {
		t.Errorf("GET %s with a service account's key = %d", publicEndpoint, r.code)
	}

	disabled := do(t, h, "PATCH", item, `{"disabled":true}`, admin...)
	if disabled.code != http.StatusOK || disabled.json["disabled"] != true {
		t.Fatalf("disable = %d %s", disabled.code, disabled.body)
	}
	keys := do(t, h, "GET", item+"/keys", "", admin...)
	if items, _ := keys.json["api_keys"].([]any); keys.code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["status"] != "revoked" || items[0].(map[string]any)["id"] != keyID {
		t.Errorf("keys after disabling = %d %s, want the key revoked", keys.code, keys.body)
	}
	if r := do(t, h, "POST", item+"/keys", fmt.Sprintf(`{"name":"sync","expires_at":%q}`, expires), admin...); r.code != http.StatusConflict || r.json["code"] != "service_account_disabled" {
		t.Errorf("key for a disabled service account = %d %s, want 409", r.code, r.body)
	}
	if r := do(t, h, "DELETE", item, "", admin...); r.code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", item, "", admin...); r.code != http.StatusNotFound || r.json["code"] != "service_account_not_found" {
		t.Errorf("get after deleting = %d %s, want 404", r.code, r.body)
	}
	noSecretsStored(t, dbURL, key)

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, action := range []string{"auth.service_account.created", "auth.service_account.disabled", "auth.service_account.deleted", "auth.api_key.created", "auth.api_key.revoked"} {
		var n int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_events WHERE action = $1`, action).Scan(&n); err != nil || n == 0 {
			t.Errorf("audit events %s = %d, %v; want at least one", action, n, err)
		}
	}
}
