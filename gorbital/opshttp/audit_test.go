package opshttp_test

import (
	"fmt"
	"net/http"
	"testing"
)

// This test is a v0.1 golden app's internal/app/audit_test.go, run against
// the library module.

func TestAuditLogThroughOps(t *testing.T) {
	a := newTestApp(t, testAppOptions{})
	a.StartWorkers(t)
	h := a.Handler()
	bearer, adminID := a.SignIn(t, "admin@example.com", "platform_admin")
	const setting = "/ops/settings/example.ping_message"

	if r := do(t, h, "PUT", setting, `{"value":"audited","version":0,"reason":"audit demo"}`, bearer...); r.Code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s", setting, r.Code, r.Body)
	}
	history := do(t, h, "GET", setting+"/history", "", bearer...)
	changes, _ := history.JSON["changes"].([]any)
	if len(changes) != 1 {
		t.Fatalf("GET %s/history = %s", setting, history.Body)
	}
	requestID, _ := changes[0].(map[string]any)["request_id"].(string)

	list := do(t, h, "GET", "/ops/audit?action=settings.value.changed", "", bearer...)
	events, _ := list.JSON["events"].([]any)
	if list.Code != http.StatusOK || len(events) != 1 {
		t.Fatalf("GET /ops/audit?action=settings.value.changed = %d %s, want one event", list.Code, list.Body)
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
	if one.Code != http.StatusOK || one.JSON["action"] != "settings.value.changed" {
		t.Errorf("GET /ops/audit/{id} = %d %s", one.Code, one.Body)
	}
	byRequest := do(t, h, "GET", "/ops/audit?request_id="+requestID, "", bearer...)
	if events, _ := byRequest.JSON["events"].([]any); len(events) != 1 {
		t.Errorf("GET /ops/audit?request_id= = %s, want the one event of that request", byRequest.Body)
	}
	if r := do(t, h, "DELETE", setting, `{"version":1}`, bearer...); r.Code != http.StatusOK {
		t.Fatalf("DELETE %s = %d %s", setting, r.Code, r.Body)
	}
	all := do(t, h, "GET", "/ops/audit?actor_id="+adminID+"&limit=1", "", bearer...)
	if events, _ := all.JSON["events"].([]any); len(events) != 1 || all.JSON["next_cursor"] == nil {
		t.Errorf("GET /ops/audit?limit=1 = %s, want one event and a next cursor", all.Body)
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
		if r.Code != tt.code || r.JSON["code"] != tt.errCode {
			t.Errorf("%s: GET %s = %d %s, want %d %s", tt.name, tt.path, r.Code, r.Body, tt.code, tt.errCode)
		}
	}
	if r := do(t, h, "GET", "/ops/audit", ""); r.Code != http.StatusUnauthorized {
		t.Errorf("GET /ops/audit without token = %d, want 401", r.Code)
	}
}
