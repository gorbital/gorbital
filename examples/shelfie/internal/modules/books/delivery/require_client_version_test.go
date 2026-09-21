package delivery

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorbital.dev/httpx"
)

// docs:start middleware-test

func TestRequireClientVersion(t *testing.T) {
	for _, tt := range []struct {
		name       string
		version    string
		wantStatus int
		wantNext   bool
	}{
		{"no version, as the web app sends", "", http.StatusNoContent, true},
		{"the version the mobile app ships", "2.4.0", http.StatusNoContent, true},
		{"exactly the minimum", "2.0.0", http.StatusNoContent, true},
		{"a version that isn't a version", "2.4", http.StatusBadRequest, false},
		{"a version with a suffix", "2.4.0-beta1", http.StatusBadRequest, false},
		{"an app from before the minimum", "1.9.9", http.StatusUpgradeRequired, false},
		{"numbers, not strings: 10 is above 2", "10.0.0", http.StatusNoContent, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reached, version := false, ClientVersion{}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				version, _ = ClientVersionFrom(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/books", nil)
			if tt.version != "" {
				req.Header.Set("X-App-Version", tt.version)
			}
			rec := httptest.NewRecorder()

			RequireClientVersion(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus || reached != tt.wantNext {
				t.Fatalf("status %d, next reached %t; want %d, %t", rec.Code, reached, tt.wantStatus, tt.wantNext)
			}
			if want := tt.version; tt.wantNext && want != "" && version.String() != want {
				t.Errorf("the handler read %s from the context, want %q", version, want)
			}
		})
	}
}

// docs:end middleware-test

// docs:start capture-test

// TestRequireClientVersionNotesFailingApps: the middleware reads the status
// the handler wrote with httpx.Capture, and adds the version to the access
// log's line for the request, so the mobile team can see which build is
// failing. Only the line worth reading carries it.
func TestRequireClientVersionNotesFailingApps(t *testing.T) {
	for _, tt := range []struct {
		name     string
		status   int
		wantLine bool
	}{
		{"a successful request", http.StatusOK, false},
		{"a refused request", http.StatusForbidden, false},
		{"the app saw a server error", http.StatusInternalServerError, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var log bytes.Buffer
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tt.status) })
			// The access log wraps the response writer first; Capture in the
			// middleware returns its record, not a second one.
			stack := httpx.AccessLog(slog.New(slog.NewJSONHandler(&log, nil)))(RequireClientVersion(handler))

			req := httptest.NewRequest(http.MethodGet, "/v1/books", nil)
			req.Header.Set("X-App-Version", "2.4.0")
			stack.ServeHTTP(httptest.NewRecorder(), req)

			if got := strings.Contains(log.String(), `"app_version":"2.4.0"`); got != tt.wantLine {
				t.Errorf("access log line carries the version = %t, want %t: %s", got, tt.wantLine, log.String())
			}
		})
	}
}

// docs:end capture-test
