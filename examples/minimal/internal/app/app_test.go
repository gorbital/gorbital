package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"apistock.dev/config"

	"example.com/acme-api/internal/app"
)

func testConfig(t *testing.T, env map[string]string) app.Config {
	t.Helper()
	cfg, err := app.LoadConfig(config.Source{
		Getenv:   func(k string) string { return env[k] },
		ReadFile: os.ReadFile,
	})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	return cfg
}

func newApp(t *testing.T, env map[string]string) http.Handler {
	t.Helper()
	ctx := context.Background()
	a, err := app.New(ctx, testConfig(t, env))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := a.Close(ctx); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return a.Handler()
}

type response struct {
	code   int
	header http.Header
	body   string
	json   map[string]any
}

func do(t *testing.T, h http.Handler, method, path, body string) response {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	r := response{code: rec.Code, header: rec.Header(), body: rec.Body.String()}
	_ = json.Unmarshal(rec.Body.Bytes(), &r.json)
	return r
}

func TestEndpoints(t *testing.T) {
	h := newApp(t, nil)

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

	if v := do(t, h, "GET", "/version", ""); v.code != 200 || v.json["version"] == nil {
		t.Errorf("GET /version = %d %s, want build information", v.code, v.body)
	}
}

func TestDocs(t *testing.T) {
	// The reference is rendered from the app's own OpenAPI document, so it
	// lists the app's endpoints (ADR-0049).
	if r := do(t, newApp(t, nil), "GET", "/docs", ""); r.code != 200 || !strings.Contains(r.body, `href="/docs/system/get-version"`) {
		t.Errorf("GET /docs with docs enabled = %d, want the API reference listing the app's endpoints", r.code)
	}
	if r := do(t, newApp(t, map[string]string{"APP_DOCS_ENABLED": "false"}), "GET", "/docs", ""); r.code != 404 {
		t.Errorf("GET /docs with docs disabled = %d, want 404", r.code)
	}
}

// TestOpenAPIUpToDate fails when api/openapi.json doesn't match the code.
func TestOpenAPIUpToDate(t *testing.T) {
	var got bytes.Buffer
	if err := app.WriteOpenAPI(context.Background(), testConfig(t, nil), &got); err != nil {
		t.Fatalf("WriteOpenAPI() error = %v", err)
	}
	path := filepath.Join("..", "..", "api", "openapi.json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v (run: go run ./cmd/api openapi > api/openapi.json)", path, err)
	}
	defer f.Close()
	want, _ := io.ReadAll(f)
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("api/openapi.json is out of date; run: go run ./cmd/api openapi > api/openapi.json")
	}
}

func TestLoadConfigReportsAllErrors(t *testing.T) {
	_, err := app.LoadConfig(config.Source{
		Getenv: func(k string) string {
			return map[string]string{
				"APP_ENV":            "staging",
				"APP_ADDR":           "8080",
				"APP_DOCS_ENABLED":   "maybe",
				"APP_MAX_BODY_BYTES": "-1",
			}[k]
		},
		ReadFile: os.ReadFile,
	})
	if err == nil {
		t.Fatal("LoadConfig(invalid values) error = nil, want error")
	}
	for _, key := range []string{"APP_ENV", "APP_ADDR", "APP_DOCS_ENABLED", "APP_MAX_BODY_BYTES"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("LoadConfig() error does not mention %s:\n%v", key, err)
		}
	}
}
