package app_test

import (
	"net/http"
	"testing"
)

func TestOpsRetention(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	if r := do(t, h, "GET", "/ops/retention", ""); r.code != http.StatusUnauthorized {
		t.Errorf("GET /ops/retention without a session = %d, want 401", r.code)
	}
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")
	r := do(t, h, "GET", "/ops/retention", "", viewer...)
	if r.code != http.StatusOK {
		t.Fatalf("GET /ops/retention = %d %s", r.code, r.body)
	}
	policies := map[string]map[string]any{}
	list, _ := r.json["policies"].([]any)
	for _, p := range list {
		policy, _ := p.(map[string]any)
		policies[policy["data"].(string)] = policy
	}

	events := policies["audit_events"]
	// Signing in recorded audit events, so the oldest is known.
	if events == nil || events["setting"] != "audit.retention" || events["job"] != "retention" ||
		events["retention_seconds"] != float64(365*24*3600) || events["oldest_at"] == nil || events["next_run_at"] == nil {
		t.Errorf("audit_events policy = %v", events)
	}
	for data, setting := range map[string]string{
		"settings_history":       "ops.history_retention",
		"job_definition_history": "ops.history_retention",
		"release_instances":      "releases.instance_retention",
		"idempotency_keys":       "idempotency.retention",
		"deleted_accounts":       "auth.deleted_account_retention",
		"unverified_accounts":    "auth.unverified_account_ttl",
	} {
		if p := policies[data]; p == nil || p["setting"] != setting || (p["job"] == nil && p["enforced_by"] == nil) {
			t.Errorf("%s policy = %v, want setting %s and what enforces it", data, p, setting)
		}
	}

	if r := do(t, h, "GET", "/ops/jobs/definitions/retention", "", viewer...); r.code != http.StatusOK {
		t.Errorf("GET /ops/jobs/definitions/retention = %d, want the retention job defined", r.code)
	}
	if r := do(t, h, "GET", "/ops/settings/audit.retention", "", viewer...); r.code != http.StatusOK || r.json["reason_required"] != true {
		t.Errorf("GET /ops/settings/audit.retention = %d %s, want a setting that needs a reason to change", r.code, r.body)
	}
}
