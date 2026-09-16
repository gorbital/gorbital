package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/config"
	"gorbital.dev/modules/postgres/pgtest"

	"example.com/acme-api/internal/app"
)

// signedInEndpoint is an endpoint any signed-in user can call (the signed-in user's organisations),
// for the API key tests in apikeys_test.go.
const signedInEndpoint = "/v1/orgs"

func testConfig(t *testing.T, env map[string]string) app.Config {
	t.Helper()
	cfg, err := app.LoadConfig(config.Source{
		Getenv: func(k string) string {
			if v, ok := env[k]; ok {
				return v
			}
			switch k {
			case "APP_ENV":
				return "development"
			case "AUTH_ENCRYPTION_KEYS":
				return testEncryptionKeys
			}
			return ""
		},
		ReadFile: os.ReadFile,
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	return cfg
}

// newApp creates a migrated database on the Docker PostgreSQL server and
// builds the app on it. Tests are skipped when the server isn't configured.
func newApp(t *testing.T, env map[string]string, configure ...func(*app.Config)) *app.App {
	t.Helper()
	a, _ := newAppWithURL(t, env, configure...)
	return a
}

// newAppWithURL is newApp that also returns the database URL, for tests that
// read tables directly. configure changes the loaded configuration.
func newAppWithURL(t *testing.T, env map[string]string, configure ...func(*app.Config)) (*app.App, string) {
	t.Helper()
	ctx := context.Background()
	url := pgtest.NewDatabase(t)
	full := map[string]string{"DATABASE_URL": url}
	maps.Copy(full, env)
	cfg := testConfig(t, full)
	for _, c := range configure {
		c(&cfg)
	}
	if err := app.Migrate(ctx, cfg, io.Discard); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	a, err := app.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(ctx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return a, url
}

type response struct {
	code   int
	header http.Header
	body   string
	json   map[string]any
}

// do sends a request; headers are name/value pairs.
func do(t *testing.T, h http.Handler, method, path, body string, headers ...string) response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
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

// personalWorkspace returns the ID of the signed-in user's personal
// workspace, created with their account.
func personalWorkspace(t *testing.T, h http.Handler, headers []string) string {
	t.Helper()
	r := do(t, h, "GET", "/v1/orgs", "", headers...)
	items, _ := r.json["items"].([]any)
	if r.code != http.StatusOK || len(items) == 0 || items[0].(map[string]any)["personal"] != true {
		t.Fatalf("GET /v1/orgs = %d %s, want the personal workspace first", r.code, r.body)
	}
	return items[0].(map[string]any)["id"].(string)
}

func TestEndpoints(t *testing.T) {
	h := newApp(t, nil).Handler()

	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		wantCode int
		wantJSON map[string]any
	}{
		{"liveness", "GET", "/livez", "", 200, map[string]any{"status": "ok"}},
		{"readiness", "GET", "/readyz", "", 200, map[string]any{"status": "ok"}},
		{"ping", "GET", "/v1/ping", "", 200, map[string]any{"message": "pong"}},
		{"echo ignores unknown fields", "POST", "/v1/echo", `{"message":" hi ","client":"ios"}`, 200, map[string]any{"message": "hi"}},
		{"domain error mapped", "POST", "/v1/echo", `{"message":"   "}`, 422, map[string]any{"code": "message_required"}},
		{"schema validation", "POST", "/v1/echo", `{"message":` + `"` + strings.Repeat("x", 501) + `"}`, 422, map[string]any{"code": "validation_failed"}},
		{"unknown route", "GET", "/v1/nope", "", 404, map[string]any{"code": "not_found"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := do(t, h, tt.method, tt.path, tt.body)
			if r.code != tt.wantCode {
				t.Fatalf("%s %s = %d %s, want %d", tt.method, tt.path, r.code, r.body, tt.wantCode)
			}
			for k, v := range tt.wantJSON {
				if r.json[k] != v {
					t.Errorf("%s %s body[%s] = %v, want %v (body %s)", tt.method, tt.path, k, r.json[k], v, r.body)
				}
			}
			if r.code >= 400 {
				if ct := r.header.Get("Content-Type"); ct != "application/problem+json" {
					t.Errorf("%s %s Content-Type = %q, want application/problem+json", tt.method, tt.path, ct)
				}
				if rid, _ := r.json["request_id"].(string); rid == "" || rid != r.header.Get("X-Request-ID") {
					t.Errorf("%s %s request_id = %q, header %q; want matching IDs", tt.method, tt.path, rid, r.header.Get("X-Request-ID"))
				}
			}
			if r.header.Get("X-Content-Type-Options") != "nosniff" {
				t.Errorf("%s %s missing security headers", tt.method, tt.path)
			}
		})
	}

	readiness := do(t, h, "GET", "/readyz", "")
	if checks, _ := readiness.json["checks"].(map[string]any); checks["postgres"] == nil {
		t.Errorf("GET /readyz = %s, want a postgres check", readiness.body)
	}
	if v := do(t, h, "GET", "/version", ""); v.code != 200 || v.json["version"] == nil {
		t.Errorf("GET /version = %d %s, want build information", v.code, v.body)
	}
}

func TestDocs(t *testing.T) {
	// The reference is rendered from the app's own OpenAPI document, so it
	// lists the app's endpoints (ADR-0049).
	if r := do(t, newApp(t, nil).Handler(), "GET", "/docs", ""); r.code != 200 || !strings.Contains(r.body, `href="/docs/system/get-version"`) {
		t.Errorf("GET /docs with docs enabled = %d, want the API reference listing the app's endpoints", r.code)
	}
	// Turning docs off hides the OpenAPI document too (HTTP-3).
	disabled := newApp(t, map[string]string{"APP_DOCS_ENABLED": "false"}).Handler()
	for _, path := range []string{"/docs", "/openapi.json", "/openapi.yaml", "/openapi-3.0.json"} {
		if r := do(t, disabled, "GET", path, ""); r.code != 404 {
			t.Errorf("GET %s with docs disabled = %d, want 404", path, r.code)
		}
	}
}

// TestRequestIDFromTrustedCallers checks that only APP_TRUSTED_CALLERS choose
// their request ID, which logs, audit events and jobs record (HTTP-5).
func TestRequestIDFromTrustedCallers(t *testing.T) {
	for _, tt := range []struct {
		callers  string
		accepted bool
	}{
		{"", false},
		{"10.0.0.0/8", false},
		{"192.0.2.0/24", true}, // httptest requests come from 192.0.2.1
	} {
		h := newApp(t, map[string]string{"APP_TRUSTED_CALLERS": tt.callers}).Handler()
		req := httptest.NewRequest("GET", "/livez", nil)
		req.Header.Set("X-Request-ID", "req_victim")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Request-ID"); (got == "req_victim") != tt.accepted || got == "" {
			t.Errorf("APP_TRUSTED_CALLERS=%q: X-Request-ID = %q, want the client's: %v", tt.callers, got, tt.accepted)
		}
	}
}

// TestOpenAPIUpToDate fails when api/openapi.json, api/postman_collection.json
// or api/llms.txt doesn't match the code.
func TestOpenAPIUpToDate(t *testing.T) {
	dir := t.TempDir()
	if err := app.WriteAPIFiles(context.Background(), testConfig(t, nil), dir); err != nil {
		t.Fatalf("WriteAPIFiles() error = %v", err)
	}
	for _, name := range []string{"openapi.json", "postman_collection.json", "llms.txt"} {
		got, _ := os.ReadFile(filepath.Join(dir, name))
		want, err := os.ReadFile(filepath.Join("..", "..", "api", name))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("api/%s is out of date; run: go run ./cmd/api openapi --dir api", name)
		}
	}
}

// TestLoadConfigSecureDefaults checks what a deployment can't get wrong
// silently: APP_ENV is required (HTTP-6), docs are off in production unless
// turned on (HTTP-3), browser origins use https in production (HTTP-4), and
// trusted callers can't be everyone.
func TestLoadConfigSecureDefaults(t *testing.T) {
	load := func(env map[string]string) (app.Config, error) {
		if env["APP_ENV"] == "production" {
			env["AUTH_ENCRYPTION_KEYS"] = testEncryptionKeys
			maps.Copy(env, mailProviderEnv)
		}
		return app.LoadConfig(config.Source{Getenv: func(k string) string { return env[k] }, ReadFile: os.ReadFile})
	}
	if _, err := load(map[string]string{"DATABASE_URL": "postgres://localhost/app"}); err == nil || !strings.Contains(err.Error(), "APP_ENV is required") {
		t.Errorf("LoadConfig() without APP_ENV error = %v, want APP_ENV is required", err)
	}
	for _, tt := range []struct {
		name     string
		env      map[string]string
		wantDocs bool
		wantErr  string
	}{
		{"development shows docs", map[string]string{"APP_ENV": "development"}, true, ""},
		{"production hides docs", map[string]string{"APP_ENV": "production"}, false, ""},
		{"production with docs turned on", map[string]string{"APP_ENV": "production", "APP_DOCS_ENABLED": "true"}, true, ""},
		{"development with docs turned off", map[string]string{"APP_ENV": "development", "APP_DOCS_ENABLED": "false"}, false, ""},
		{"http origin in development", map[string]string{"APP_ENV": "development", "APP_CORS_ORIGINS": "http://localhost:3000"}, true, ""},
		{"https origin in production", map[string]string{"APP_ENV": "production", "APP_CORS_ORIGINS": "https://app.example.com"}, false, ""},
		{"http origin in production", map[string]string{"APP_ENV": "production", "APP_CORS_ORIGINS": "https://app.example.com, http://app.example.com"}, false, "APP_CORS_ORIGINS"},
		{"every address as trusted callers", map[string]string{"APP_ENV": "development", "APP_TRUSTED_CALLERS": "0.0.0.0/0"}, true, "APP_TRUSTED_CALLERS"},
	} {
		cfg, err := load(tt.env)
		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("%s: LoadConfig() error = %v", tt.name, err)
		case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
			t.Errorf("%s: LoadConfig() error = %v, want one mentioning %s", tt.name, err, tt.wantErr)
		case err == nil && cfg.DocsEnabled != tt.wantDocs:
			t.Errorf("%s: DocsEnabled = %v, want %v", tt.name, cfg.DocsEnabled, tt.wantDocs)
		}
	}
}

func TestLoadConfigReportsAllErrors(t *testing.T) {
	_, err := app.LoadConfig(config.Source{
		Getenv: func(k string) string {
			return map[string]string{
				"APP_ENV":              "staging",
				"APP_ADDR":             "8080",
				"APP_DOCS_ENABLED":     "maybe",
				"APP_MAX_BODY_BYTES":   "-1",
				"APP_DB_MAX_CONNS":     "0",
				"APP_JOB_WORKERS":      "many",
				"MAIL_DELIVERY":        "provider",
				"AUTH_ENCRYPTION_KEYS": "k1:not-a-key",
			}[k]
		},
		ReadFile: os.ReadFile,
	})
	if err == nil {
		t.Fatal("LoadConfig(invalid values) error = nil, want error")
	}
	for _, key := range []string{"APP_ENV", "APP_ADDR", "APP_DOCS_ENABLED", "APP_MAX_BODY_BYTES", "APP_DB_MAX_CONNS", "APP_JOB_WORKERS", mailProviderRequired, "AUTH_ENCRYPTION_KEYS"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("LoadConfig() error does not mention %s:\n%v", key, err)
		}
	}
}

func TestNewAndMigrateRequireDatabaseURL(t *testing.T) {
	cfg := testConfig(t, nil)
	if _, err := app.New(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("New() without DATABASE_URL error = %v", err)
	}
	if err := app.Migrate(context.Background(), cfg, io.Discard); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("Migrate() without DATABASE_URL error = %v", err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t, map[string]string{"DATABASE_URL": pgtest.NewDatabase(t)})
	var first, second bytes.Buffer
	if err := app.Migrate(ctx, cfg, &first); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if !strings.Contains(first.String(), "applied migration 20260914000001") || !strings.Contains(first.String(), "applied job queue migration") {
		t.Errorf("first Migrate() output = %q, want app and job queue migrations", first.String())
	}
	if err := app.Migrate(ctx, cfg, &second); err != nil || second.Len() != 0 {
		t.Errorf("second Migrate() = %q, %v; want nothing applied", second.String(), err)
	}
}

// projectsOf returns the collection of the projects in the signed-in user's
// personal workspace.
func projectsOf(t *testing.T, h http.Handler, headers []string) string {
	t.Helper()
	return "/v1/orgs/" + personalWorkspace(t, h, headers) + "/projects"
}
