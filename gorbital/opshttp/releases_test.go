package opshttp_test

import (
	"net/http"
	"testing"

	"gorbital.dev/gorbital/internal/opstest"
)

// This test is a v0.1 golden app's internal/app/releases_test.go, run
// against the library module.

// TestReleasesThroughOps runs the app's workers, so the release tracker
// records this instance, and reads it through /ops/releases (ADR-0040).
func TestReleasesThroughOps(t *testing.T) {
	a := opstest.New(t, opstest.Options{})
	a.StartWorkers(t)
	h := a.Handler()
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")

	var current opstest.Response
	opstest.WaitFor(t, "this instance's release", func() bool {
		current = opstest.Do(t, h, "GET", "/ops/releases/current", "", viewer...)
		list, _ := current.JSON["releases"].([]any)
		return current.Code == http.StatusOK && len(list) == 1
	})
	release := current.JSON["releases"].([]any)[0].(map[string]any)
	instances, _ := release["instances"].([]any)
	if release["version"] == "" || len(instances) != 1 || instances[0].(map[string]any)["running"] != true ||
		instances[0].(map[string]any)["started_at"] == nil {
		t.Errorf("GET /ops/releases/current = %s, want one running instance", current.Body)
	}

	list := opstest.Do(t, h, "GET", "/ops/releases", "", viewer...)
	releases, _ := list.JSON["releases"].([]any)
	if list.Code != http.StatusOK || len(releases) != 1 || releases[0].(map[string]any)["running"] != float64(1) ||
		releases[0].(map[string]any)["starts"] != float64(1) {
		t.Errorf("GET /ops/releases = %d %s, want one release running on one instance", list.Code, list.Body)
	}
	running := opstest.Do(t, h, "GET", "/ops/releases/instances?running=true&version="+release["version"].(string), "", viewer...)
	if items, _ := running.JSON["instances"].([]any); running.Code != http.StatusOK || len(items) != 1 {
		t.Errorf("GET /ops/releases/instances?running=true = %d %s, want this instance", running.Code, running.Body)
	}

	for _, path := range []string{"/ops/releases?cursor=abc", "/ops/releases/instances?cursor=abc"} {
		if r := opstest.Do(t, h, "GET", path, "", viewer...); r.Code != http.StatusBadRequest || r.JSON["code"] != "invalid_cursor" {
			t.Errorf("GET %s = %d %s, want 400 invalid_cursor", path, r.Code, r.Body)
		}
	}
	noRole, _ := a.SignIn(t, "user@example.com", "")
	if r := opstest.Do(t, h, "GET", "/ops/releases/current", "", noRole...); r.Code != http.StatusForbidden {
		t.Errorf("GET /ops/releases/current without a role = %d, want 403", r.Code)
	}
}
