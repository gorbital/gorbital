package app_test

import (
	"net/http"
	"testing"
)

// TestReleasesThroughOps runs the app's workers, so the release tracker
// records this instance, and reads it through /ops/releases (ADR-0040).
func TestReleasesThroughOps(t *testing.T) {
	a := newApp(t, nil)
	startWorkers(t, a)
	h := a.Handler()
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")

	var current response
	waitFor(t, "this instance's release", func() bool {
		current = do(t, h, "GET", "/ops/releases/current", "", viewer...)
		list, _ := current.json["releases"].([]any)
		return current.code == http.StatusOK && len(list) == 1
	})
	release := current.json["releases"].([]any)[0].(map[string]any)
	instances, _ := release["instances"].([]any)
	if release["version"] == "" || len(instances) != 1 || instances[0].(map[string]any)["running"] != true ||
		instances[0].(map[string]any)["started_at"] == nil {
		t.Errorf("GET /ops/releases/current = %s, want one running instance", current.body)
	}

	list := do(t, h, "GET", "/ops/releases", "", viewer...)
	releases, _ := list.json["releases"].([]any)
	if list.code != http.StatusOK || len(releases) != 1 || releases[0].(map[string]any)["running"] != float64(1) ||
		releases[0].(map[string]any)["starts"] != float64(1) {
		t.Errorf("GET /ops/releases = %d %s, want one release running on one instance", list.code, list.body)
	}
	running := do(t, h, "GET", "/ops/releases/instances?running=true&version="+release["version"].(string), "", viewer...)
	if items, _ := running.json["instances"].([]any); running.code != http.StatusOK || len(items) != 1 {
		t.Errorf("GET /ops/releases/instances?running=true = %d %s, want this instance", running.code, running.body)
	}

	for _, path := range []string{"/ops/releases?cursor=abc", "/ops/releases/instances?cursor=abc"} {
		if r := do(t, h, "GET", path, "", viewer...); r.code != http.StatusBadRequest || r.json["code"] != "invalid_cursor" {
			t.Errorf("GET %s = %d %s, want 400 invalid_cursor", path, r.code, r.body)
		}
	}
	noRole, _ := signIn(t, a, "user@example.com", "")
	if r := do(t, h, "GET", "/ops/releases/current", "", noRole...); r.code != http.StatusForbidden {
		t.Errorf("GET /ops/releases/current without a role = %d, want 403", r.code)
	}
}
