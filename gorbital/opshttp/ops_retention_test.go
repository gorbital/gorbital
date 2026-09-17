package opshttp_test

import (
	"net/http"
	"testing"
)

// This test is a v0.1 golden app's internal/app/ops_retention_test.go, run
// against the library module.

func TestOpsRetention(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	h := a.Handler()
	if r := do(t, h, "GET", "/ops/retention", ""); r.Code != http.StatusUnauthorized {
		t.Errorf("GET /ops/retention without a session = %d, want 401", r.Code)
	}
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	// Sign-in isn't a module yet (Phase 5), so signing in records no audit
	// event: an administrator's setting change records one instead.
	admin, _ := a.SignIn(t, "admin@example.com", "platform_admin")
	if r := do(t, h, "PUT", "/ops/settings/example.ping_message", `{"value":"audited","version":0,"reason":"retention"}`, admin...); r.Code != http.StatusOK {
		t.Fatalf("PUT /ops/settings/example.ping_message = %d %s", r.Code, r.Body)
	}
	r := do(t, h, "GET", "/ops/retention", "", viewer...)
	if r.Code != http.StatusOK {
		t.Fatalf("GET /ops/retention = %d %s", r.Code, r.Body)
	}
	policies := map[string]map[string]any{}
	list, _ := r.JSON["policies"].([]any)
	for _, p := range list {
		policy, _ := p.(map[string]any)
		policies[policy["data"].(string)] = policy
	}

	events := policies["audit_events"]
	// The setting change recorded an audit event, so the oldest is known.
	if events == nil || events["setting"] != "audit.retention" || events["job"] != "retention" ||
		events["retention_seconds"] != float64(365*24*3600) || events["oldest_at"] == nil || events["next_run_at"] == nil {
		t.Errorf("audit_events policy = %v", events)
	}
	for data, setting := range map[string]string{
		"settings_history":       "ops.history_retention",
		"flags_history":          "ops.history_retention",
		"job_definition_history": "ops.history_retention",
		"release_instances":      "releases.instance_retention",
		"idempotency_keys":       "idempotency.retention",
		"observability_minutes":  "observability.retention",
		// deleted_accounts (auth.deleted_account_retention) and
		// unverified_accounts (auth.unverified_account_ttl) are sign-in's
		// policies, which come with it in Phase 5.
	} {
		if p := policies[data]; p == nil || p["setting"] != setting || (p["job"] == nil && p["enforced_by"] == nil) {
			t.Errorf("%s policy = %v, want setting %s and what enforces it", data, p, setting)
		}
	}

	if r := do(t, h, "GET", "/ops/jobs/definitions/retention", "", viewer...); r.Code != http.StatusOK {
		t.Errorf("GET /ops/jobs/definitions/retention = %d, want the retention job defined", r.Code)
	}
	if r := do(t, h, "GET", "/ops/settings/audit.retention", "", viewer...); r.Code != http.StatusOK || r.JSON["reason_required"] != true {
		t.Errorf("GET /ops/settings/audit.retention = %d %s, want a setting that needs a reason to change", r.Code, r.Body)
	}
}
