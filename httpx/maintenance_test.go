package httpx_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/httpx"
)

// switchValue is a config.Value[bool] a test turns on and off, as a runtime
// setting changes.
type switchValue struct{ on atomic.Bool }

func (s *switchValue) Get(context.Context) bool { return s.on.Load() }

func TestMaintenance(t *testing.T) {
	enabled := &switchValue{}
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := httpx.Maintenance(httpx.MaintenanceOptions{
		Enabled:    enabled,
		Message:    config.Static("Upgrading the database"),
		RetryAfter: config.Static(5 * time.Minute),
		Open:       []string{"/livez", "/readyz", "/docs", "/docs/", "/ops/"},
	})(ok)

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/v1/books"); rec.Code != http.StatusNoContent {
		t.Fatalf("GET /v1/books while off = %d, want 204", rec.Code)
	}

	enabled.on.Store(true)
	rec := get("/v1/books")
	var p httpx.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || p.Code != "maintenance" || p.Detail != "Upgrading the database" || rec.Header().Get("Retry-After") != "300" {
		t.Errorf("GET /v1/books while on = %d %+v, Retry-After %q; want 503 maintenance with the message and 300", rec.Code, p, rec.Header().Get("Retry-After"))
	}
	for _, path := range []string{"/livez", "/readyz", "/docs", "/docs/assets/app.js", "/ops/settings"} {
		if rec := get(path); rec.Code != http.StatusNoContent {
			t.Errorf("GET %s while on = %d, want it open", path, rec.Code)
		}
	}
	for _, path := range []string{"/livez/x", "/opsx", "/ops", "/documents"} {
		if rec := get(path); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s while on = %d, want 503: only paths ending in a slash cover what is under them", path, rec.Code)
		}
	}
}

func TestMaintenanceDefaults(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	tests := []struct {
		name       string
		opts       httpx.MaintenanceOptions
		wantStatus int
		wantDetail string
		wantRetry  string
	}{
		{"no switch never blocks", httpx.MaintenanceOptions{}, http.StatusNoContent, "", ""},
		{"empty message", httpx.MaintenanceOptions{Enabled: config.Static(true), Message: config.Static("")}, http.StatusServiceUnavailable, httpx.DefaultMaintenanceMessage, ""},
		{"no retry under a second", httpx.MaintenanceOptions{Enabled: config.Static(true), RetryAfter: config.Static(time.Millisecond)}, http.StatusServiceUnavailable, httpx.DefaultMaintenanceMessage, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			httpx.Maintenance(tt.opts)(ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			var p httpx.Problem
			_ = json.Unmarshal(rec.Body.Bytes(), &p)
			if rec.Code != tt.wantStatus || p.Detail != tt.wantDetail || rec.Header().Get("Retry-After") != tt.wantRetry {
				t.Errorf("= %d %q Retry-After %q; want %d %q %q", rec.Code, p.Detail, rec.Header().Get("Retry-After"), tt.wantStatus, tt.wantDetail, tt.wantRetry)
			}
		})
	}
}

func ExampleMaintenance() {
	books := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "books") })
	// In an app, Enabled, Message and RetryAfter are runtime settings, so
	// operators switch every instance without a restart.
	h := httpx.Maintenance(httpx.MaintenanceOptions{
		Enabled:    config.Static(true),
		RetryAfter: config.Static(5 * time.Minute),
		Open:       []string{"/livez", "/readyz"},
	})(books)

	for _, path := range []string{"/v1/books", "/readyz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		fmt.Println(path, rec.Code, rec.Header().Get("Retry-After"))
	}
	// Output:
	// /v1/books 503 300
	// /readyz 200
}

func ExampleMaintenanceOptions() {
	opts := httpx.MaintenanceOptions{
		Enabled: config.Static(true),
		Message: config.Static("Back at 10:00 UTC"),
		// Health checks exactly, and everything under /ops/.
		Open: []string{"/livez", "/readyz", "/ops/"},
	}
	rec := httptest.NewRecorder()
	httpx.Maintenance(opts)(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/books", nil))
	fmt.Println(rec.Code, rec.Header().Get("Content-Type"))
	// Output: 503 application/problem+json
}
