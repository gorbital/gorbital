package app_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"gorbital.dev/spikes/openapi/internal/app"
)

// TestPrintSamples logs real responses for the spike write-up.
// Run: go test ./internal/app -run TestPrintSamples -v
func TestPrintSamples(t *testing.T) {
	h, _ := app.New(slog.New(slog.DiscardHandler))
	for _, s := range []struct{ method, path, token, body string }{
		{"POST", "/v1/orgs/org_acme/projects", "alice", `{"name":"Website"}`},
		{"POST", "/v1/orgs/org_acme/projects", "alice", `{"name":"Website"}`},
		{"POST", "/v1/orgs/org_acme/projects", "alice", `{"name":"","extra":1}`},
		{"GET", "/v1/orgs/org_acme/projects", "mallory", ""},
	} {
		req := httptest.NewRequest(s.method, s.path, strings.NewReader(s.body))
		req.Header.Set("Authorization", "Bearer "+s.token)
		if s.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		body, _ := io.ReadAll(rec.Body)
		t.Logf("%s %s → %d %s\n%s", s.method, s.path, rec.Code, rec.Header().Get("Content-Type"), body)
	}
}
