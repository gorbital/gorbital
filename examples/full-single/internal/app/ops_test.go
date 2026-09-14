package app_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"apistock.dev/actor"

	"example.com/acme-api/internal/app"
)

const testPassword = "correct horse battery"

// signIn creates a verified account holding role (none when empty), signs
// it in with a bearer token, and returns the header to send and the user ID.
func signIn(t *testing.T, a *app.App, email, role string) ([]string, string) {
	t.Helper()
	ctx := actor.With(context.Background(), actor.System("test"))
	u, err := a.Auth().CreateUser(ctx, email, testPassword, true)
	if err != nil {
		t.Fatalf("CreateUser(%s) error = %v", email, err)
	}
	if role != "" {
		if err := a.Auth().GrantRole(ctx, u.ID, role); err != nil {
			t.Fatalf("GrantRole(%s) error = %v", role, err)
		}
	}
	r := do(t, a.Handler(), "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q,"transport":"bearer"}`, email, testPassword))
	token, _ := r.json["token"].(string)
	if r.code != http.StatusOK || token == "" {
		t.Fatalf("POST /v1/auth/login as %s = %d %s", email, r.code, r.body)
	}
	return []string{"Authorization", "Bearer " + token}, u.ID
}

// startWorkers runs the app's background runners until the test ends.
func startWorkers(t *testing.T, a *app.App) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, len(a.Workers()))
	for _, r := range a.Workers() {
		go func() { done <- r.Run(ctx) }()
	}
	t.Cleanup(func() {
		cancel()
		for range a.Workers() {
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("worker Run() error = %v", err)
				}
			case <-time.After(30 * time.Second):
				t.Error("a worker did not stop")
				return
			}
		}
	})
}

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

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestOpsRoutesRequirePermissions(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	for name, headers := range map[string][]string{
		"no token":      nil,
		"unknown token": {"Authorization", "Bearer not-a-session-token"},
		"basic auth":    {"Authorization", "Basic dXNlcjpwYXNz"},
	} {
		if r := do(t, h, "GET", "/ops/settings", "", headers...); r.code != http.StatusUnauthorized || r.json["code"] != "unauthenticated" {
			t.Errorf("%s: GET /ops/settings = %d %s, want 401 unauthenticated", name, r.code, r.body)
		}
	}

	noRole, _ := signIn(t, a, "user@example.com", "")
	if r := do(t, h, "GET", "/ops/settings", "", noRole...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("GET /ops/settings without a role = %d %s, want 403 forbidden", r.code, r.body)
	}
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")
	if r := do(t, h, "GET", "/ops/settings", "", viewer...); r.code != http.StatusOK {
		t.Errorf("GET /ops/settings as ops_viewer = %d %s, want 200", r.code, r.body)
	}
	if r := do(t, h, "PUT", "/ops/settings/example.ping_message", `{"value":"hi","version":0}`, viewer...); r.code != http.StatusForbidden {
		t.Errorf("PUT /ops/settings as ops_viewer = %d %s, want 403", r.code, r.body)
	}
	admin, _ := signIn(t, a, "admin@example.com", "platform_admin")
	if r := do(t, h, "POST", "/ops/mail/test", `{"to":"ops@example.com"}`, admin...); r.code != http.StatusAccepted {
		t.Errorf("POST /ops/mail/test as platform_admin = %d %s, want 202", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/ping", ""); r.code != http.StatusOK {
		t.Errorf("GET /v1/ping without a session = %d, want public routes unaffected", r.code)
	}
}

func TestRuntimeSettingsThroughOps(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	bearer, adminID := signIn(t, a, "admin@example.com", "platform_admin")
	const path = "/ops/settings/example.ping_message"

	list := do(t, h, "GET", "/ops/settings", "", bearer...)
	settings, _ := list.json["settings"].([]any)
	if len(settings) == 0 || settings[0].(map[string]any)["key"] != "example.ping_message" {
		t.Fatalf("GET /ops/settings = %s", list.body)
	}

	r := do(t, h, "PUT", path, `{"value":"hello","version":0,"reason":"demo"}`, bearer...)
	if r.code != http.StatusOK || r.json["value"] != "hello" || r.json["version"] != float64(1) || r.json["modified"] != true || r.json["updated_by"] != adminID {
		t.Fatalf("PUT %s = %d %s", path, r.code, r.body)
	}
	if ping := do(t, h, "GET", "/v1/ping", ""); ping.json["message"] != "hello" {
		t.Errorf("GET /v1/ping after the change = %s, want hello", ping.body)
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
		if r.code != tt.code || r.json["code"] != tt.errCode {
			t.Errorf("%s: %s %s = %d %s, want %d %s", tt.name, tt.method, tt.path, r.code, r.body, tt.code, tt.errCode)
		}
	}

	history := do(t, h, "GET", path+"/history", "", bearer...)
	changes, _ := history.json["changes"].([]any)
	if len(changes) != 1 || changes[0].(map[string]any)["new_value"] != "hello" || changes[0].(map[string]any)["actor_id"] != adminID {
		t.Errorf("GET %s/history = %s", path, history.body)
	}

	reset := do(t, h, "DELETE", path, `{"version":1}`, bearer...)
	if reset.code != http.StatusOK || reset.json["modified"] != false || reset.json["value"] != "pong" {
		t.Errorf("DELETE %s = %d %s", path, reset.code, reset.body)
	}
	if ping := do(t, h, "GET", "/v1/ping", ""); ping.json["message"] != "pong" {
		t.Errorf("GET /v1/ping after reset = %s, want pong", ping.body)
	}
}

func TestJobsThroughOps(t *testing.T) {
	a := newApp(t, nil)
	startWorkers(t, a)
	h := a.Handler()
	bearer, adminID := signIn(t, a, "admin@example.com", "platform_admin")
	const def = "/ops/jobs/definitions/heartbeat"

	// Look jobs up by name: `aps gen job` adds more definitions to this app.
	list := do(t, h, "GET", "/ops/jobs/definitions", "", bearer...)
	heartbeat := findDefinition(list.json, "heartbeat")
	if heartbeat == nil {
		t.Fatalf("GET /ops/jobs/definitions = %s, want the heartbeat job", list.body)
	}
	config := heartbeat["config"].(map[string]any)
	if heartbeat["name"] != "heartbeat" || config["schedule"] != "@every 1h" || config["timeout"] != "1m0s" || heartbeat["next_run_at"] == nil {
		t.Errorf("heartbeat definition = %v", heartbeat)
	}

	if r := do(t, h, "PUT", def, `{"schedule":"@every 2h","version":0}`, bearer...); r.code != 422 || r.json["code"] != "job_reason_required" {
		t.Errorf("reschedule without reason = %d %s", r.code, r.body)
	}
	if r := do(t, h, "PUT", def, `{"timeout":"soon","version":0}`, bearer...); r.code != 422 || r.json["code"] != "invalid_job_config" {
		t.Errorf("invalid timeout = %d %s", r.code, r.body)
	}
	if r := do(t, h, "PUT", def, `{"max_attempts":500,"version":0}`, bearer...); r.code != 422 {
		t.Errorf("max_attempts above schema maximum = %d %s", r.code, r.body)
	}
	r := do(t, h, "PUT", def, `{"schedule":"@every 2h","timeout":"30s","version":0,"reason":"less noise"}`, bearer...)
	if cfg, _ := r.json["config"].(map[string]any); r.code != 200 || cfg["schedule"] != "@every 2h" || cfg["timeout"] != "30s" || r.json["version"] != float64(1) {
		t.Fatalf("PUT %s = %d %s", def, r.code, r.body)
	}

	run := do(t, h, "POST", def+"/run", "", bearer...)
	if run.code != http.StatusAccepted || run.json["kind"] != "heartbeat" || run.json["actor_id"] != adminID {
		t.Fatalf("POST %s/run = %d %s", def, run.code, run.body)
	}
	runPath := fmt.Sprintf("/ops/jobs/runs/%.0f", run.json["id"])
	waitFor(t, "the heartbeat job to complete", func() bool {
		return do(t, h, "GET", runPath, "", bearer...).json["state"] == "completed"
	})

	runs := do(t, h, "GET", "/ops/jobs/runs?kind=heartbeat&state=completed", "", bearer...)
	if jobs, _ := runs.json["jobs"].([]any); runs.code != 200 || len(jobs) != 1 {
		t.Errorf("GET /ops/jobs/runs = %d %s, want the completed run", runs.code, runs.body)
	}
	if r := do(t, h, "GET", "/ops/jobs/runs?state=exploded", "", bearer...); r.code != 422 || r.json["code"] != "invalid_job_state" {
		t.Errorf("unknown state filter = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", "/ops/jobs/runs/999999999", "", bearer...); r.code != 404 || r.json["code"] != "job_not_found" {
		t.Errorf("missing run = %d %s", r.code, r.body)
	}

	if r := do(t, h, "PUT", def, `{"enabled":false,"version":1,"reason":"maintenance"}`, bearer...); r.code != 200 || r.json["next_run_at"] != nil {
		t.Fatalf("disable = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", def+"/run", "", bearer...); r.code != 409 || r.json["code"] != "job_definition_disabled" {
		t.Errorf("run disabled job = %d %s", r.code, r.body)
	}
	if scheduled := do(t, h, "GET", "/ops/jobs/scheduled", "", bearer...); findDefinition(scheduled.json, "heartbeat") != nil {
		t.Errorf("GET /ops/jobs/scheduled = %s, want the disabled heartbeat job left out", scheduled.body)
	}
	history := do(t, h, "GET", def+"/history", "", bearer...)
	if changes, _ := history.json["changes"].([]any); len(changes) != 2 {
		t.Errorf("GET %s/history = %s, want 2 changes", def, history.body)
	}

	waitFor(t, "the default queue to be active", func() bool {
		queues, _ := do(t, h, "GET", "/ops/queues", "", bearer...).json["queues"].([]any)
		return len(queues) == 1
	})
	if r := do(t, h, "POST", "/ops/queues/default/pause", "", bearer...); r.code != http.StatusNoContent {
		t.Errorf("pause = %d %s", r.code, r.body)
	}
	queues := do(t, h, "GET", "/ops/queues", "", bearer...).json["queues"].([]any)
	if queues[0].(map[string]any)["paused"] != true {
		t.Errorf("queue after pause = %v", queues[0])
	}
	if r := do(t, h, "POST", "/ops/queues/default/resume", "", bearer...); r.code != http.StatusNoContent {
		t.Errorf("resume = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/ops/queues/reports/pause", "", bearer...); r.code != 422 || r.json["code"] != "queue_not_active" {
		t.Errorf("pause inactive queue = %d %s", r.code, r.body)
	}
}
