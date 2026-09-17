package opshttp_test

import (
	"fmt"
	"net/http"
	"slices"
	"testing"
)

// testEmailsPerHour is how many test emails an operator may send an hour.
const testEmailsPerHour = 5

// These tests are a v0.1 golden app's internal/app/ops_test.go, run against
// the library module (roadmap item 56).

// findDefinition returns the named job definition from a definitions list
// response, or nil.
func findDefinition(body map[string]any, name string) map[string]any {
	defs, _ := body["definitions"].([]any)
	for _, d := range defs {
		if def, ok := d.(map[string]any); ok && def["name"] == name {
			return def
		}
	}
	return nil
}

func TestOpsRoutesRequirePermissions(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	h := a.Handler()
	for name, headers := range map[string][]string{
		"no token":      nil,
		"unknown token": {"Authorization", "Bearer not-a-session-token"},
		"basic auth":    {"Authorization", "Basic dXNlcjpwYXNz"},
	} {
		if r := do(t, h, "GET", "/ops/settings", "", headers...); r.Code != http.StatusUnauthorized || r.JSON["code"] != "unauthenticated" {
			t.Errorf("%s: GET /ops/settings = %d %s, want 401 unauthenticated", name, r.Code, r.Body)
		}
	}

	noRole, _ := a.SignIn(t, "user@example.com", "")
	if r := do(t, h, "GET", "/ops/settings", "", noRole...); r.Code != http.StatusForbidden || r.JSON["code"] != "forbidden" {
		t.Errorf("GET /ops/settings without a role = %d %s, want 403 forbidden", r.Code, r.Body)
	}
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	if r := do(t, h, "GET", "/ops/settings", "", viewer...); r.Code != http.StatusOK {
		t.Errorf("GET /ops/settings as ops_viewer = %d %s, want 200", r.Code, r.Body)
	}
	if r := do(t, h, "PUT", "/ops/settings/example.ping_message", `{"value":"hi","version":0}`, viewer...); r.Code != http.StatusForbidden {
		t.Errorf("PUT /ops/settings as ops_viewer = %d %s, want 403", r.Code, r.Body)
	}
	admin, _ := a.SignIn(t, "admin@example.com", "platform_admin")
	if r := do(t, h, "POST", "/ops/mail/test", `{"to":"ops@example.com"}`, admin...); r.Code != http.StatusAccepted || r.JSON["delivery"] != "devmail" {
		t.Errorf("POST /ops/mail/test as platform_admin = %d %s, want 202", r.Code, r.Body)
	}
	// Test emails are limited per operator (security review OPS-8).
	for range testEmailsPerHour - 1 {
		do(t, h, "POST", "/ops/mail/test", `{"to":"ops@example.com"}`, admin...)
	}
	if r := do(t, h, "POST", "/ops/mail/test", `{"to":"ops@example.com"}`, admin...); r.Code != http.StatusTooManyRequests || r.JSON["code"] != "rate_limited" {
		t.Errorf("POST /ops/mail/test over the hourly limit = %d %s, want 429 rate_limited", r.Code, r.Body)
	}
	if r := do(t, h, "GET", "/v1/ping", ""); r.Code != http.StatusOK {
		t.Errorf("GET /v1/ping without a session = %d, want public routes unaffected", r.Code)
	}
}

func TestRuntimeSettingsThroughOps(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	h := a.Handler()
	bearer, adminID := a.SignIn(t, "admin@example.com", "platform_admin")
	const path = "/ops/settings/example.ping_message"

	list := do(t, h, "GET", "/ops/settings", "", bearer...)
	// The golden app declared its settings first; gorbital's built-in
	// settings come before the modules'.
	settings, _ := list.JSON["settings"].([]any)
	if !slices.ContainsFunc(settings, func(s any) bool { return s.(map[string]any)["key"] == "example.ping_message" }) {
		t.Fatalf("GET /ops/settings = %s", list.Body)
	}

	r := do(t, h, "PUT", path, `{"value":"hello","version":0,"reason":"demo"}`, append(bearer, "User-Agent", "ops-console/1")...)
	if r.Code != http.StatusOK || r.JSON["value"] != "hello" || r.JSON["version"] != float64(1) || r.JSON["modified"] != true || r.JSON["updated_by"] != adminID {
		t.Fatalf("PUT %s = %d %s", path, r.Code, r.Body)
	}
	// Audit events of ops changes say where the request came from (security
	// review OPS-7).
	changed := do(t, h, "GET", "/ops/audit?action=settings.value.changed", "", bearer...)
	if events, _ := changed.JSON["events"].([]any); len(events) != 1 ||
		events[0].(map[string]any)["ip"] != "192.0.2.1" || events[0].(map[string]any)["user_agent"] != "ops-console/1" {
		t.Errorf("GET /ops/audit?action=settings.value.changed = %s, want the client's IP and user agent", changed.Body)
	}
	if ping := do(t, h, "GET", "/v1/ping", ""); ping.JSON["message"] != "hello" {
		t.Errorf("GET /v1/ping after the change = %s, want hello", ping.Body)
	}

	errorsWanted := []struct {
		name, method, path, body string
		code                     int
		errCode                  string
	}{
		{"stale version", "PUT", path, `{"value":"again","version":0}`, 409, "setting_version_conflict"},
		{"blank value", "PUT", path, `{"value":"   ","version":1}`, 422, "invalid_setting_value"},
		{"wrong type", "PUT", path, `{"value":42,"version":1}`, 422, "invalid_setting_value"},
		{"unknown key", "PUT", "/ops/settings/example.nope", `{"value":"x","version":0}`, 404, "setting_not_found"},
	}
	for _, tt := range errorsWanted {
		r := do(t, h, tt.method, tt.path, tt.body, bearer...)
		if r.Code != tt.code || r.JSON["code"] != tt.errCode {
			t.Errorf("%s: %s %s = %d %s, want %d %s", tt.name, tt.method, tt.path, r.Code, r.Body, tt.code, tt.errCode)
		}
	}

	history := do(t, h, "GET", path+"/history", "", bearer...)
	changes, _ := history.JSON["changes"].([]any)
	if len(changes) != 1 || changes[0].(map[string]any)["new_value"] != "hello" || changes[0].(map[string]any)["actor_id"] != adminID {
		t.Errorf("GET %s/history = %s", path, history.Body)
	}

	reset := do(t, h, "DELETE", path, `{"version":1}`, bearer...)
	if reset.Code != http.StatusOK || reset.JSON["modified"] != false || reset.JSON["value"] != "pong" {
		t.Errorf("DELETE %s = %d %s", path, reset.Code, reset.Body)
	}
	if ping := do(t, h, "GET", "/v1/ping", ""); ping.JSON["message"] != "pong" {
		t.Errorf("GET /v1/ping after reset = %s, want pong", ping.Body)
	}
}

func TestJobsThroughOps(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	a.StartWorkers(t)
	h := a.Handler()
	bearer, adminID := a.SignIn(t, "admin@example.com", "platform_admin")
	const def = "/ops/jobs/definitions/heartbeat"

	// Look jobs up by name: `orb gen job` adds more definitions to this app.
	list := do(t, h, "GET", "/ops/jobs/definitions", "", bearer...)
	heartbeat := findDefinition(list.JSON, "heartbeat")
	if heartbeat == nil {
		t.Fatalf("GET /ops/jobs/definitions = %s, want the heartbeat job", list.Body)
	}
	config := heartbeat["config"].(map[string]any)
	if heartbeat["name"] != "heartbeat" || config["schedule"] != "@every 1h" || config["timeout"] != "1m0s" || heartbeat["next_run_at"] == nil {
		t.Errorf("heartbeat definition = %v", heartbeat)
	}

	if r := do(t, h, "PUT", def, `{"schedule":"@every 2h","version":0}`, bearer...); r.Code != 422 || r.JSON["code"] != "job_reason_required" {
		t.Errorf("reschedule without reason = %d %s", r.Code, r.Body)
	}
	if r := do(t, h, "PUT", def, `{"timeout":"soon","version":0}`, bearer...); r.Code != 422 || r.JSON["code"] != "invalid_job_config" {
		t.Errorf("invalid timeout = %d %s", r.Code, r.Body)
	}
	if r := do(t, h, "PUT", def, `{"max_attempts":500,"version":0}`, bearer...); r.Code != 422 {
		t.Errorf("max_attempts above schema maximum = %d %s", r.Code, r.Body)
	}
	// A 1-second timeout stops a job as surely as disabling it (security
	// review OPS-2).
	if r := do(t, h, "PUT", def, `{"timeout":"1s","version":0}`, bearer...); r.Code != 422 || r.JSON["code"] != "job_reason_required" {
		t.Errorf("timeout without reason = %d %s", r.Code, r.Body)
	}
	r := do(t, h, "PUT", def, `{"schedule":"@every 2h","timeout":"30s","version":0,"reason":"less noise"}`, bearer...)
	if cfg, _ := r.JSON["config"].(map[string]any); r.Code != 200 || cfg["schedule"] != "@every 2h" || cfg["timeout"] != "30s" || r.JSON["version"] != float64(1) {
		t.Fatalf("PUT %s = %d %s", def, r.Code, r.Body)
	}

	run := do(t, h, "POST", def+"/run", "", bearer...)
	if run.Code != http.StatusAccepted || run.JSON["kind"] != "heartbeat" || run.JSON["actor_id"] != adminID {
		t.Fatalf("POST %s/run = %d %s", def, run.Code, run.Body)
	}
	runPath := fmt.Sprintf("/ops/jobs/runs/%.0f", run.JSON["id"])
	waitFor(t, "the heartbeat job to complete", func() bool {
		return do(t, h, "GET", runPath, "", bearer...).JSON["state"] == "completed"
	})
	// On-demand runs can't pile up, and a completed run never runs again
	// (security review OPS-2, OPS-3).
	if r := do(t, h, "POST", def+"/run", "", bearer...); r.Code != http.StatusTooManyRequests || r.JSON["code"] != "job_run_limited" {
		t.Errorf("second run within a minute = %d %s, want 429 job_run_limited", r.Code, r.Body)
	}
	if r := do(t, h, "POST", runPath+"/retry", "", bearer...); r.Code != http.StatusConflict || r.JSON["code"] != "job_not_retryable" {
		t.Errorf("retry a completed run = %d %s, want 409 job_not_retryable", r.Code, r.Body)
	}

	runs := do(t, h, "GET", "/ops/jobs/runs?kind=heartbeat&state=completed", "", bearer...)
	if jobs, _ := runs.JSON["jobs"].([]any); runs.Code != 200 || len(jobs) != 1 {
		t.Errorf("GET /ops/jobs/runs = %d %s, want the completed run", runs.Code, runs.Body)
	}
	if r := do(t, h, "GET", "/ops/jobs/runs?state=exploded", "", bearer...); r.Code != 422 || r.JSON["code"] != "invalid_job_state" {
		t.Errorf("unknown state filter = %d %s", r.Code, r.Body)
	}
	if r := do(t, h, "GET", "/ops/jobs/runs/999999999", "", bearer...); r.Code != 404 || r.JSON["code"] != "job_not_found" {
		t.Errorf("missing run = %d %s", r.Code, r.Body)
	}

	if r := do(t, h, "PUT", def, `{"enabled":false,"version":1,"reason":"maintenance"}`, bearer...); r.Code != 200 || r.JSON["next_run_at"] != nil {
		t.Fatalf("disable = %d %s", r.Code, r.Body)
	}
	if r := do(t, h, "POST", def+"/run", "", bearer...); r.Code != 409 || r.JSON["code"] != "job_definition_disabled" {
		t.Errorf("run disabled job = %d %s", r.Code, r.Body)
	}
	if scheduled := do(t, h, "GET", "/ops/jobs/scheduled", "", bearer...); findDefinition(scheduled.JSON, "heartbeat") != nil {
		t.Errorf("GET /ops/jobs/scheduled = %s, want the disabled heartbeat job left out", scheduled.Body)
	}
	history := do(t, h, "GET", def+"/history", "", bearer...)
	if changes, _ := history.JSON["changes"].([]any); len(changes) != 2 {
		t.Errorf("GET %s/history = %s, want 2 changes", def, history.Body)
	}

	waitFor(t, "the default queue to be active", func() bool {
		queues, _ := do(t, h, "GET", "/ops/queues", "", bearer...).JSON["queues"].([]any)
		return len(queues) == 1
	})
	// Pausing stops email delivery among everything else (security review
	// OPS-2).
	if r := do(t, h, "POST", "/ops/queues/default/pause", "", bearer...); r.Code != 422 || r.JSON["code"] != "job_reason_required" {
		t.Errorf("pause without reason = %d %s, want 422 job_reason_required", r.Code, r.Body)
	}
	if r := do(t, h, "POST", "/ops/queues/default/pause", `{"reason":"provider outage"}`, bearer...); r.Code != http.StatusNoContent {
		t.Errorf("pause = %d %s", r.Code, r.Body)
	}
	queues := do(t, h, "GET", "/ops/queues", "", bearer...).JSON["queues"].([]any)
	if queues[0].(map[string]any)["paused"] != true {
		t.Errorf("queue after pause = %v", queues[0])
	}
	if r := do(t, h, "POST", "/ops/queues/default/resume", "", bearer...); r.Code != http.StatusNoContent {
		t.Errorf("resume = %d %s", r.Code, r.Body)
	}
	paused := do(t, h, "GET", "/ops/audit?action=jobs.queue.paused", "", bearer...)
	if events, _ := paused.JSON["events"].([]any); len(events) != 1 || events[0].(map[string]any)["metadata"].(map[string]any)["reason"] != "provider outage" {
		t.Errorf("GET /ops/audit?action=jobs.queue.paused = %s, want the reason", paused.Body)
	}
	if r := do(t, h, "POST", "/ops/queues/reports/pause", `{"reason":"test"}`, bearer...); r.Code != 422 || r.JSON["code"] != "queue_not_active" {
		t.Errorf("pause inactive queue = %d %s", r.Code, r.Body)
	}
}
