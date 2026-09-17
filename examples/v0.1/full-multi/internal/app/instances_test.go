package app_test

import (
	"context"
	"net/http"
	"testing"

	"example.com/acme-api/internal/app"
)

// TestSettingChangeReachesAnotherInstance runs two instances on one
// database: a setting changed through /ops/settings on one is served by the
// other without a restart, and both see it in the history and audit log.
func TestSettingChangeReachesAnotherInstance(t *testing.T) {
	ctx := context.Background()
	first, url := newAppWithURL(t, nil)
	second, err := app.New(ctx, testConfig(t, map[string]string{"DATABASE_URL": url}))
	if err != nil {
		t.Fatalf("New() second instance error = %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(ctx); err != nil {
			t.Errorf("Close() second instance error = %v", err)
		}
	})
	startWorkers(t, second)
	// Sessions live in the database, so the token works on both instances.
	bearer, adminID := signIn(t, first, "admin@example.com", "platform_admin")
	const path = "/ops/settings/example.ping_message"

	r := do(t, first.Handler(), "PUT", path, `{"value":"set on the first instance","version":0,"reason":"two instances"}`, bearer...)
	if r.code != http.StatusOK {
		t.Fatalf("PUT %s on the first instance = %d %s", path, r.code, r.body)
	}
	waitFor(t, "the second instance to serve the new value", func() bool {
		return do(t, second.Handler(), "GET", "/v1/ping", "").json["message"] == "set on the first instance"
	})

	history := do(t, second.Handler(), "GET", path+"/history", "", bearer...)
	changes, _ := history.json["changes"].([]any)
	if len(changes) != 1 || changes[0].(map[string]any)["actor_id"] != adminID || changes[0].(map[string]any)["reason"] != "two instances" {
		t.Errorf("GET %s/history on the second instance = %s", path, history.body)
	}
	list := do(t, second.Handler(), "GET", "/ops/audit?action=settings.value.changed&resource_id=example.ping_message", "", bearer...)
	events, _ := list.json["events"].([]any)
	if list.code != http.StatusOK || len(events) != 1 || events[0].(map[string]any)["actor_id"] != adminID {
		t.Errorf("GET /ops/audit on the second instance = %d %s, want the change's event", list.code, list.body)
	}
}
