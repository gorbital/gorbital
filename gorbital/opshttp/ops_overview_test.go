package opshttp_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"gorbital.dev/gorbital/internal/opstest"
)

// This test is a v0.1 golden app's internal/app/ops_overview_test.go, run
// against the library module.

func TestOpsAuditStatsAndJobsOverview(t *testing.T) {
	a := opstest.New(t, opstest.Options{})
	h := a.Handler()
	for _, path := range []string{"/ops/audit/stats?group_by=action", "/ops/jobs/overview"} {
		if r := opstest.Do(t, h, "GET", path, ""); r.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without a session = %d, want 401", path, r.Code)
		}
	}
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	// Sign-in isn't a module yet (Phase 5), so signing in records no audit
	// event: an administrator's setting change records one instead.
	admin, _ := a.SignIn(t, "admin@example.com", "platform_admin")
	if r := opstest.Do(t, h, "PUT", "/ops/settings/example.ping_message", `{"value":"audited","version":0,"reason":"stats"}`, admin...); r.Code != http.StatusOK {
		t.Fatalf("PUT /ops/settings/example.ping_message = %d %s", r.Code, r.Body)
	}

	// The setting change recorded audit events in the last 7 days.
	r := opstest.Do(t, h, "GET", "/ops/audit/stats?group_by=outcome", "", viewer...)
	groups, _ := r.JSON["groups"].([]any)
	if total, _ := r.JSON["total"].(float64); r.Code != http.StatusOK || total == 0 || len(groups) == 0 || r.JSON["group_by"] != "outcome" {
		t.Errorf("GET /ops/audit/stats?group_by=outcome = %d %s", r.Code, r.Body)
	}
	from := url.QueryEscape(time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339))
	if r := opstest.Do(t, h, "GET", "/ops/audit/stats?group_by=day&from="+from, "", viewer...); r.Code != http.StatusUnprocessableEntity || r.JSON["code"] != "invalid_audit_filter" {
		t.Errorf("GET /ops/audit/stats over 90 days = %d %s, want 422 invalid_audit_filter", r.Code, r.Body)
	}
	if r := opstest.Do(t, h, "GET", "/ops/audit/stats?group_by=actor_id", "", viewer...); r.Code != http.StatusUnprocessableEntity {
		t.Errorf("GET /ops/audit/stats?group_by=actor_id = %d %s, want 422", r.Code, r.Body)
	}

	r = opstest.Do(t, h, "GET", "/ops/jobs/overview", "", viewer...)
	if _, ok := r.JSON["queues"].([]any); r.Code != http.StatusOK || !ok {
		t.Errorf("GET /ops/jobs/overview = %d %s, want queues as a list", r.Code, r.Body)
	}
	if _, ok := r.JSON["failing"].([]any); !ok {
		t.Errorf("GET /ops/jobs/overview failing = %v, want a list", r.JSON["failing"])
	}
}
