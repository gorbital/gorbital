package app_test

import (
	"fmt"
	"net/http"
	"testing"
)

func TestAuditLogThroughOps(t *testing.T) {
	a := newApp(t, nil)
	startWorkers(t, a)
	h := a.Handler()
	bearer, adminID := signIn(t, a, "admin@example.com", "platform_admin")
	const setting = "/ops/settings/example.ping_message"

	if r := do(t, h, "PUT", setting, `{"value":"audited","version":0,"reason":"audit demo"}`, bearer...); r.code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s", setting, r.code, r.body)
	}
	history := do(t, h, "GET", setting+"/history", "", bearer...)
	changes, _ := history.json["changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("GET %s/history = %s", setting, history.body)
	}
	requestID, _ := changes[0].(map[string]any)["request_id"].(string)

	list := do(t, h, "GET", "/ops/audit?action=settings.value.changed", "", bearer...)
	events, _ := list.json["events"].([]any)
	if list.code != http.StatusOK || len(events) != 1 {
		t.Fatalf("GET /ops/audit?action=settings.value.changed = %d %s, want one event", list.code, list.body)
	}
	event := events[0].(map[string]any)
	metadata, _ := event["metadata"].(map[string]any)
	if event["actor_kind"] != "user" || event["actor_id"] != adminID || event["resource_type"] != "setting" ||
		event["resource_id"] != "example.ping_message" || event["outcome"] != "success" ||
		metadata["reason"] != "audit demo" || metadata["version"] != float64(1) {
		t.Errorf("audit event = %v", event)
	}
	if requestID == "" || event["request_id"] != requestID {
		t.Errorf("audit event request_id = %v, want the change's request ID %q", event["request_id"], requestID)
	}

	one := do(t, h, "GET", fmt.Sprintf("/ops/audit/%.0f", event["id"]), "", bearer...)
	if one.code != http.StatusOK || one.json["action"] != "settings.value.changed" {
		t.Errorf("GET /ops/audit/{id} = %d %s", one.code, one.body)
	}
	byRequest := do(t, h, "GET", "/ops/audit?request_id="+requestID, "", bearer...)
	if events, _ := byRequest.json["events"].([]any); len(events) != 1 {
		t.Errorf("GET /ops/audit?request_id= = %s, want the one event of that request", byRequest.body)
	}
	if r := do(t, h, "DELETE", setting, `{"version":1}`, bearer...); r.code != http.StatusOK {
		t.Fatalf("DELETE %s = %d %s", setting, r.code, r.body)
	}
	all := do(t, h, "GET", "/ops/audit?actor_id="+adminID+"&limit=1", "", bearer...)
	if events, _ := all.json["events"].([]any); len(events) != 1 || all.json["next_cursor"] == nil {
		t.Errorf("GET /ops/audit?limit=1 = %s, want one event and a next cursor", all.body)
	}

	errorsWanted := []struct {
		name, path string
		code       int
		errCode    string
	}{
		{"missing event", "/ops/audit/999999999", 404, "audit_event_not_found"},
		{"bad cursor", "/ops/audit?cursor=abc", 400, "invalid_cursor"},
		{"bad outcome", "/ops/audit?outcome=maybe", 422, "invalid_audit_filter"},
		{"empty range", "/ops/audit?from=2026-09-02T00:00:00Z&to=2026-09-01T00:00:00Z", 422, "invalid_audit_filter"},
	}
	for _, tt := range errorsWanted {
		r := do(t, h, "GET", tt.path, "", bearer...)
		if r.code != tt.code || r.json["code"] != tt.errCode {
			t.Errorf("%s: GET %s = %d %s, want %d %s", tt.name, tt.path, r.code, r.body, tt.code, tt.errCode)
		}
	}
	if r := do(t, h, "GET", "/ops/audit", ""); r.code != http.StatusUnauthorized {
		t.Errorf("GET /ops/audit without token = %d, want 401", r.code)
	}
}
