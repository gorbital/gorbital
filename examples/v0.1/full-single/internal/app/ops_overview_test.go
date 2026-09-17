package app_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestOpsAuditStatsAndJobsOverview(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	for _, path := range []string{"/ops/audit/stats?group_by=action", "/ops/jobs/overview"} {
		if r := do(t, h, "GET", path, ""); r.code != http.StatusUnauthorized {
			t.Errorf("GET %s without a session = %d, want 401", path, r.code)
		}
	}
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")

	// Signing in recorded audit events in the last 7 days.
	r := do(t, h, "GET", "/ops/audit/stats?group_by=outcome", "", viewer...)
	groups, _ := r.json["groups"].([]any)
	if total, _ := r.json["total"].(float64); r.code != http.StatusOK || total == 0 || len(groups) == 0 || r.json["group_by"] != "outcome" {
		t.Errorf("GET /ops/audit/stats?group_by=outcome = %d %s", r.code, r.body)
	}
	from := url.QueryEscape(time.Now().Add(-100 * 24 * time.Hour).UTC().Format(time.RFC3339))
	if r := do(t, h, "GET", "/ops/audit/stats?group_by=day&from="+from, "", viewer...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "invalid_audit_filter" {
		t.Errorf("GET /ops/audit/stats over 90 days = %d %s, want 422 invalid_audit_filter", r.code, r.body)
	}
	if r := do(t, h, "GET", "/ops/audit/stats?group_by=actor_id", "", viewer...); r.code != http.StatusUnprocessableEntity {
		t.Errorf("GET /ops/audit/stats?group_by=actor_id = %d %s, want 422", r.code, r.body)
	}

	r = do(t, h, "GET", "/ops/jobs/overview", "", viewer...)
	if _, ok := r.json["queues"].([]any); r.code != http.StatusOK || !ok {
		t.Errorf("GET /ops/jobs/overview = %d %s, want queues as a list", r.code, r.body)
	}
	if _, ok := r.json["failing"].([]any); !ok {
		t.Errorf("GET /ops/jobs/overview failing = %v, want a list", r.json["failing"])
	}
}
