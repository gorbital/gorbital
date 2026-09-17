package flagshttp_test

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/flags"
)

// These tests are a v0.1 golden app's internal/app/flags_test.go, run
// against the library modules: flagshttp serves GET /v1/flags and opshttp
// serves /ops/flags, so the test covers both end to end.

const pingTimeFlag = "/ops/flags/example.ping_time"

// flagState returns a PUT /ops/flags/{key} body.
func flagState(version int, reason, state string) string {
	return fmt.Sprintf(`{"version":%d,"reason":%q,"state":%s}`, version, reason, state)
}

// pingHasTime reports whether GET /v1/ping includes the server's time, the
// example.ping_time feature, for the caller signed in with headers.
func pingHasTime(t *testing.T, h http.Handler, headers ...string) bool {
	t.Helper()
	r := do(t, h, "GET", "/v1/ping", "", headers...)
	if r.Code != http.StatusOK {
		t.Fatalf("GET /v1/ping = %d %s", r.Code, r.Body)
	}
	_, ok := r.JSON["server_time"]
	return ok
}

// clientFlags returns GET /v1/flags for the caller signed in with headers.
func clientFlags(t *testing.T, h http.Handler, path string, headers ...string) map[string]any {
	t.Helper()
	r := do(t, h, "GET", path, "", headers...)
	got, _ := r.JSON["flags"].(map[string]any)
	if r.Code != http.StatusOK || got == nil || r.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("GET %s = %d %s (Cache-Control %q), want the caller's flags, not cached", path, r.Code, r.Body, r.Header.Get("Cache-Control"))
	}
	return got
}

// TestFeatureFlagsEndToEnd follows ADR-0057 over HTTP: operators read and
// change flags with a version and a reason, every change is bounded, in the
// history and audited, and the example flag changes GET /v1/ping and
// GET /v1/flags for exactly the callers its rules pick.
func TestFeatureFlagsEndToEnd(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	h := a.Handler()
	admin, adminID := a.SignIn(t, "admin@example.com", "platform_admin")
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	ada, adaID := a.SignIn(t, "ada@example.com", "")
	bob, bobID := a.SignIn(t, "bob@example.com", "")

	// Permissions: operators read, only ops.flags.write changes.
	for _, tt := range []struct {
		name, method, path, body string
		headers                  []string
		code                     int
		errCode                  string
	}{
		{"without a session", "GET", "/ops/flags", "", nil, 401, "unauthenticated"},
		{"a user without ops roles", "GET", "/ops/flags", "", ada, 403, "forbidden"},
		{"a viewer changes a flag", "PUT", pingTimeFlag, flagState(0, "x", `{"enabled":true,"default":true,"orgs":{"allow":[],"deny":[]},"users":{"allow":[],"deny":[]}}`), viewer, 403, "forbidden"},
		{"a viewer resets a flag", "DELETE", pingTimeFlag, `{"version":0,"reason":"x"}`, viewer, 403, "forbidden"},
		{"clients without a session", "GET", "/v1/flags", "", nil, 401, "unauthenticated"},
	} {
		if r := do(t, h, tt.method, tt.path, tt.body, tt.headers...); r.Code != tt.code || r.JSON["code"] != tt.errCode {
			t.Errorf("%s: %s %s = %d %s, want %d %s", tt.name, tt.method, tt.path, r.Code, r.Body, tt.code, tt.errCode)
		}
	}

	list := do(t, h, "GET", "/ops/flags", "", viewer...)
	listed, _ := list.JSON["flags"].([]any)
	if list.Code != http.StatusOK || len(listed) == 0 {
		t.Fatalf("GET /ops/flags as a viewer = %d %s", list.Code, list.Body)
	}
	clientKeys := map[string]bool{}
	for _, f := range listed {
		if flag := f.(map[string]any); flag["client"] == true {
			clientKeys[flag["key"].(string)] = true
		}
	}
	flag := do(t, h, "GET", pingTimeFlag, "", viewer...)
	if state, _ := flag.JSON["state"].(map[string]any); flag.Code != http.StatusOK || flag.JSON["client"] != true || flag.JSON["version"] != float64(0) ||
		flag.JSON["modified"] != false || state["enabled"] != false || state["percentage"] != nil {
		t.Errorf("GET %s = %d %s, want the declared state: disabled", pingTimeFlag, flag.Code, flag.Body)
	}

	// Clients get only the flags declared flags.Client(), evaluated for them.
	got := clientFlags(t, h, "/v1/flags", ada...)
	if len(got) != len(clientKeys) || got["example.ping_time"] != false {
		t.Errorf("GET /v1/flags = %v, want exactly the client flags %v, off", got, clientKeys)
	}
	for key := range got {
		if !clientKeys[key] {
			t.Errorf("GET /v1/flags lists %s, which isn't declared flags.Client()", key)
		}
	}
	if pingHasTime(t, h, ada...) || pingHasTime(t, h) {
		t.Error("GET /v1/ping includes the server's time while the flag is off")
	}

	// Rejected changes: reason, bounds, conflicts, unknown flags.
	empty := `"orgs":{"allow":[],"deny":[]}`
	tooMany := make([]string, 1001)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("%q", fmt.Sprintf("usr_%d", i))
	}
	for _, tt := range []struct {
		name, method, path, body string
		code                     int
		errCode                  string
	}{
		{"no reason", "PUT", pingTimeFlag, flagState(0, " ", `{"enabled":true,"default":true,`+empty+`,"users":{"allow":[],"deny":[]}}`), 422, "flag_reason_required"},
		{"percentage above 100", "PUT", pingTimeFlag, flagState(0, "x", `{"enabled":true,"default":false,`+empty+`,"users":{"allow":[],"deny":[]},"percentage":101}`), 422, "validation_failed"},
		{"negative percentage", "PUT", pingTimeFlag, flagState(0, "x", `{"enabled":true,"default":false,`+empty+`,"users":{"allow":[],"deny":[]},"percentage":-1}`), 422, "validation_failed"},
		{"1001 users", "PUT", pingTimeFlag, flagState(0, "x", `{"enabled":true,"default":false,`+empty+`,"users":{"allow":[`+strings.Join(tooMany, ",")+`],"deny":[]}}`), 422, "validation_failed"},
		{"an ID in both lists", "PUT", pingTimeFlag, flagState(0, "x", `{"enabled":true,"default":false,`+empty+`,"users":{"allow":["usr_1"],"deny":["usr_1"]}}`), 422, "invalid_flag_state"},
		{"an ID with a space", "PUT", pingTimeFlag, flagState(0, "x", `{"enabled":true,"default":false,`+empty+`,"users":{"allow":["usr 1"],"deny":[]}}`), 422, "invalid_flag_state"},
		{"an unknown flag", "PUT", "/ops/flags/example.nope", flagState(0, "x", `{"enabled":true,"default":false,`+empty+`,"users":{"allow":[],"deny":[]}}`), 404, "flag_not_found"},
		{"an unknown flag's history", "GET", "/ops/flags/example.nope/history", "", 404, "flag_not_found"},
		{"a reset without a reason", "DELETE", pingTimeFlag, `{"version":0,"reason":""}`, 422, "flag_reason_required"},
	} {
		if r := do(t, h, tt.method, tt.path, tt.body, admin...); r.Code != tt.code || r.JSON["code"] != tt.errCode {
			t.Errorf("%s: %s %s = %d %s, want %d %s", tt.name, tt.method, tt.path, r.Code, r.Body, tt.code, tt.errCode)
		}
	}
	if r := do(t, h, "GET", pingTimeFlag, "", admin...); r.JSON["version"] != float64(0) {
		t.Fatalf("after rejected changes, GET %s = %s, want version 0", pingTimeFlag, r.Body)
	}

	// Turned on for Ada only: a 0% rollout for everyone else.
	set := do(t, h, "PUT", pingTimeFlag, flagState(0, "try it with Ada",
		`{"enabled":true,"default":false,"orgs":{"allow":[],"deny":[]},"users":{"allow":[`+fmt.Sprintf("%q", adaID)+`],"deny":[]},"percentage":0}`), admin...)
	if state, _ := set.JSON["state"].(map[string]any); set.Code != http.StatusOK || set.JSON["version"] != float64(1) || set.JSON["modified"] != true ||
		set.JSON["updated_by"] != adminID || state["percentage"] != float64(0) {
		t.Fatalf("PUT %s = %d %s", pingTimeFlag, set.Code, set.Body)
	}
	if !pingHasTime(t, h, ada...) || pingHasTime(t, h, bob...) || pingHasTime(t, h) {
		t.Error("with Ada allowed and a 0% rollout, only Ada should get the server's time")
	}
	if clientFlags(t, h, "/v1/flags", ada...)["example.ping_time"] != true || clientFlags(t, h, "/v1/flags", bob...)["example.ping_time"] != false {
		t.Error("GET /v1/flags doesn't follow the user allow list")
	}
	if r := do(t, h, "PUT", pingTimeFlag, flagState(0, "stale", `{"enabled":false,"default":false,"orgs":{"allow":[],"deny":[]},"users":{"allow":[],"deny":[]}}`), admin...); r.Code != http.StatusConflict || r.JSON["code"] != "flag_version_conflict" {
		t.Errorf("PUT with a stale version = %d %s, want 409 flag_version_conflict", r.Code, r.Body)
	}

	// A 50% rollout: signed-in callers get their own bucket, every time;
	// anonymous callers get the default.
	fifty := func(version int, def bool) {
		t.Helper()
		body := flagState(version, "half", fmt.Sprintf(`{"enabled":true,"default":%v,"orgs":{"allow":[],"deny":[]},"users":{"allow":[],"deny":[]},"percentage":50}`, def))
		if r := do(t, h, "PUT", pingTimeFlag, body, admin...); r.Code != http.StatusOK {
			t.Fatalf("PUT %s = %d %s", pingTimeFlag, r.Code, r.Body)
		}
	}
	fifty(1, false)
	for _, caller := range []struct {
		id      string
		headers []string
	}{{adaID, ada}, {bobID, bob}} {
		want := flags.Bucket("example.ping_time", caller.id) < 50
		for range 3 {
			if got := pingHasTime(t, h, caller.headers...); got != want {
				t.Errorf("%s in a 50%% rollout: server time = %v, want %v from its bucket", caller.id, got, want)
			}
		}
	}
	if pingHasTime(t, h) {
		t.Error("an anonymous caller in a 50% rollout with default false got the feature")
	}
	fifty(2, true)
	if !pingHasTime(t, h) {
		t.Error("an anonymous caller in a 50% rollout with default true didn't get the default")
	}

	history := do(t, h, "GET", pingTimeFlag+"/history", "", viewer...)
	changes, _ := history.JSON["changes"].([]any)
	if history.Code != http.StatusOK || len(changes) != 3 {
		t.Fatalf("GET %s/history = %d %s, want 3 changes", pingTimeFlag, history.Code, history.Body)
	}
	if first := changes[2].(map[string]any); first["reason"] != "try it with Ada" || first["actor_id"] != adminID || first["old_state"] != nil || first["new_state"] == nil {
		t.Errorf("first change = %v", first)
	}

	reset := do(t, h, "DELETE", pingTimeFlag, `{"version":3,"reason":"experiment over"}`, admin...)
	if state, _ := reset.JSON["state"].(map[string]any); reset.Code != http.StatusOK || reset.JSON["version"] != float64(4) || reset.JSON["modified"] != false || state["enabled"] != false {
		t.Errorf("DELETE %s = %d %s, want the declared state at version 4", pingTimeFlag, reset.Code, reset.Body)
	}
	if pingHasTime(t, h, ada...) {
		t.Error("after the reset, Ada still gets the feature")
	}

	for action, want := range map[string]int{"flags.flag.changed": 3, "flags.flag.reset": 1} {
		r := do(t, h, "GET", "/ops/audit?action="+action+"&resource_id=example.ping_time", "", admin...)
		events, _ := r.JSON["events"].([]any)
		if r.Code != http.StatusOK || len(events) != want || events[0].(map[string]any)["actor_id"] != adminID {
			t.Errorf("GET /ops/audit?action=%s = %d %s, want %d events by the operator", action, r.Code, r.Body, want)
		}
		if strings.Contains(r.Body, adaID) {
			t.Errorf("audit events for %s list targeted user IDs", action)
		}
	}
}

// TestFlagChangeReachesAnotherInstance runs two instances on one database: a
// flag changed through /ops/flags on one is applied by the other without a
// restart.
func TestFlagChangeReachesAnotherInstance(t *testing.T) {
	first := newTestApp(t, testAppOptions{})
	env := map[string]string{
		"APP_ENV": "development", "APP_ADDR": "127.0.0.1:0", "DATABASE_URL": first.URL, "APP_DB_MAX_CONNS": "8", "APP_JOB_WORKERS": "4",
		"LOG_ARCHIVE_DIR": t.TempDir(), "STORAGE_LOCAL_DIR": t.TempDir(),
	}
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(key string) string { return env[key] }, ReadFile: os.ReadFile})
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	second, err := buildTestApp(t, cfg, testAppOptions{})
	if err != nil {
		t.Fatalf("New() second instance error = %v", err)
	}
	second.StartWorkers(t)
	admin, _ := first.SignIn(t, "admin@example.com", "platform_admin")
	// Harness tokens are per instance: the same operator signs in to the
	// second instance too.
	secondAdmin, _ := second.SignIn(t, "admin@example.com", "platform_admin")

	body := flagState(0, "two instances", `{"enabled":true,"default":false,"orgs":{"allow":[],"deny":[]},"users":{"allow":[],"deny":[]},"percentage":100}`)
	if r := do(t, first.Handler(), "PUT", pingTimeFlag, body, admin...); r.Code != http.StatusOK {
		t.Fatalf("PUT %s on the first instance = %d %s", pingTimeFlag, r.Code, r.Body)
	}
	waitFor(t, "the second instance to apply the flag", func() bool { return pingHasTime(t, second.Handler()) })
	if r := do(t, second.Handler(), "GET", pingTimeFlag, "", secondAdmin...); r.JSON["version"] != float64(1) {
		t.Errorf("GET %s on the second instance = %s, want version 1", pingTimeFlag, r.Body)
	}

	if r := do(t, first.Handler(), "DELETE", pingTimeFlag, `{"version":1,"reason":"roll back"}`, admin...); r.Code != http.StatusOK {
		t.Fatalf("DELETE %s on the first instance = %d %s", pingTimeFlag, r.Code, r.Body)
	}
	waitFor(t, "the second instance to apply the reset", func() bool { return !pingHasTime(t, second.Handler()) })
}
