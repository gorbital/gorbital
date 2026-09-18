package orgshttp

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"gorbital.dev/actor"
)

// invitationTTL returns how long an invitation created in the response r
// stays valid.
func invitationTTL(t *testing.T, r response) time.Duration {
	t.Helper()
	created, err1 := time.Parse(time.RFC3339Nano, fmt.Sprint(r.json["created_at"]))
	expires, err2 := time.Parse(time.RFC3339Nano, fmt.Sprint(r.json["expires_at"]))
	if r.code != http.StatusCreated || err1 != nil || err2 != nil {
		t.Fatalf("invite = %d %s", r.code, r.body)
	}
	return expires.Sub(created)
}

// TestOrganisationSettingsEndToEnd follows ADR-0056 over HTTP: members read
// their organisation's settings, owners and admins set its own values within
// the platform's constraints, the value applies to that organisation only,
// operators see every organisation's values, and purging the organisation
// removes them.
func TestOrganisationSettingsEndToEnd(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	pool := newPool(t, dbURL)
	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, _ := signIn(t, a, "bob@example.com", "")
	ops, _ := signIn(t, a, "ops@example.com", "platform_admin")

	created := do(t, h, "POST", "/v1/orgs", `{"name":"Road Runners"}`, ada...)
	org, _ := created.json["id"].(string)
	if created.code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.code, created.body)
	}
	base := "/v1/orgs/" + org
	do(t, h, "POST", base+"/invitations", `{"email":"bob@example.com"}`, ada...)
	if r := do(t, h, "POST", "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, emailedInvitation(t, a, "bob@example.com")), bob...); r.code != http.StatusOK {
		t.Fatalf("bob accepts = %d %s", r.code, r.body)
	}

	list := do(t, h, "GET", base+"/settings", "", bob...)
	items, _ := list.json["items"].([]any)
	if list.code != http.StatusOK || len(items) != 1 {
		t.Fatalf("GET %s/settings as a member = %d %s, want only the organisation settings", base, list.code, list.body)
	}
	if s := items[0].(map[string]any); s["key"] != "orgs.invitation_ttl" || s["value"] != "168h0m0s" || s["platform_value"] != "168h0m0s" || s["overridden"] != false || s["version"] != float64(0) {
		t.Errorf("organisation setting = %v", s)
	}

	path := base + "/settings/orgs.invitation_ttl"
	errorsWanted := []struct {
		name, method, path, body string
		headers                  []string
		code                     int
		errCode                  string
	}{
		{"member changes it", "PUT", path, `{"value":"48h","version":0,"reason":"x"}`, bob, 403, "forbidden"},
		{"member resets it", "DELETE", path, `{"version":0,"reason":"x"}`, bob, 403, "forbidden"},
		{"no reason", "PUT", path, `{"value":"48h","version":0}`, ada, 422, "setting_reason_required"},
		{"outside the platform range", "PUT", path, `{"value":"1h","version":0,"reason":"x"}`, ada, 422, "invalid_setting_value"},
		{"a platform-only setting", "PUT", base + "/settings/auth.session_idle_ttl", `{"value":"2h","version":0,"reason":"x"}`, ada, 404, "setting_not_found"},
		{"a platform-only setting's history", "GET", base + "/settings/orgs.invitation_url/history", "", ada, 404, "setting_not_found"},
		{"an unknown setting", "GET", base + "/settings/example.nope", "", ada, 404, "setting_not_found"},
		{"without signing in", "GET", base + "/settings", "", nil, 401, "unauthenticated"},
	}
	for _, tt := range errorsWanted {
		if r := do(t, h, tt.method, tt.path, tt.body, tt.headers...); r.code != tt.code || r.json["code"] != tt.errCode {
			t.Errorf("%s: %s %s = %d %s, want %d %s", tt.name, tt.method, tt.path, r.code, r.body, tt.code, tt.errCode)
		}
	}

	set := do(t, h, "PUT", path, `{"value":"48h","version":0,"reason":"links for events"}`, ada...)
	if set.code != http.StatusOK || set.json["value"] != "48h0m0s" || set.json["platform_value"] != "168h0m0s" || set.json["overridden"] != true || set.json["version"] != float64(1) {
		t.Fatalf("PUT %s = %d %s", path, set.code, set.body)
	}
	if r := do(t, h, "PUT", path, `{"value":"72h","version":0,"reason":"stale"}`, ada...); r.code != http.StatusConflict || r.json["code"] != "setting_version_conflict" {
		t.Errorf("PUT with a stale version = %d %s, want 409 setting_version_conflict", r.code, r.body)
	}

	// The organisation's invitations use its value; other organisations keep
	// the platform value.
	if ttl := invitationTTL(t, do(t, h, "POST", base+"/invitations", `{"email":"dave@example.com"}`, ada...)); ttl != 48*time.Hour {
		t.Errorf("invitation in the organisation valid for %v, want its own 48h", ttl)
	}
	personal := do(t, h, "POST", "/v1/orgs", `{"name":"Ada's other team"}`, ada...)
	other, _ := personal.json["id"].(string)
	if ttl := invitationTTL(t, do(t, h, "POST", "/v1/orgs/"+other+"/invitations", `{"email":"dave@example.com"}`, ada...)); ttl != 7*24*time.Hour {
		t.Errorf("invitation in another organisation valid for %v, want the platform 168h", ttl)
	}

	history := do(t, h, "GET", path+"/history", "", bob...)
	if changes, _ := history.json["items"].([]any); history.code != http.StatusOK || len(changes) != 1 ||
		changes[0].(map[string]any)["new_value"] != "48h0m0s" || changes[0].(map[string]any)["reason"] != "links for events" {
		t.Errorf("GET %s/history = %d %s", path, history.code, history.body)
	}

	// Operators see which organisations have their own value, and the audit
	// event names the organisation.
	overrides := do(t, h, "GET", "/ops/settings/orgs.invitation_ttl/overrides", "", ops...)
	if found, _ := overrides.json["overrides"].([]any); overrides.code != http.StatusOK || len(found) != 1 ||
		found[0].(map[string]any)["org_id"] != org || found[0].(map[string]any)["value"] != "48h0m0s" {
		t.Errorf("GET /ops/settings/orgs.invitation_ttl/overrides = %d %s", overrides.code, overrides.body)
	}
	if r := do(t, h, "GET", "/ops/settings/orgs.invitation_ttl", "", ops...); r.json["org_overridable"] != true || r.json["modified"] != false {
		t.Errorf("GET /ops/settings/orgs.invitation_ttl = %s, want org_overridable and the platform value unchanged", r.body)
	}
	audit := do(t, h, "GET", "/ops/audit?action=settings.value.changed&org_id="+org, "", ops...)
	if events, _ := audit.json["events"].([]any); len(events) != 1 {
		t.Errorf("GET /ops/audit for the organisation's setting change = %s, want 1 event", audit.body)
	}

	reset := do(t, h, "DELETE", path, `{"version":1,"reason":"events are over"}`, ada...)
	if reset.code != http.StatusOK || reset.json["overridden"] != false || reset.json["value"] != "168h0m0s" || reset.json["version"] != float64(2) {
		t.Errorf("DELETE %s = %d %s", path, reset.code, reset.body)
	}
	if r := do(t, h, "PUT", path, `{"value":"96h","version":2,"reason":"again"}`, ada...); r.code != http.StatusOK {
		t.Fatalf("PUT again = %d %s", r.code, r.body)
	}

	// Purging the organisation removes its values and their history.
	ctx := actor.With(context.Background(), actor.System("test"))
	if r := do(t, h, "DELETE", base, "", ada...); r.code != http.StatusNoContent {
		t.Fatalf("delete the organisation = %d %s", r.code, r.body)
	}
	if _, err := pool.Exec(ctx, `UPDATE orgs SET purge_after = now() - interval '1 minute' WHERE id = $1`, org); err != nil {
		t.Fatal(err)
	}
	if n, err := a.Orgs().Purge(ctx); err != nil || n != 1 {
		t.Fatalf("Purge() = %d, %v; want 1", n, err)
	}
	var values, changes int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM settings_values WHERE org_id = $1), (SELECT count(*) FROM settings_history WHERE org_id = $1)`, org).Scan(&values, &changes); err != nil || values != 0 || changes != 0 {
		t.Errorf("after purging, the organisation has %d setting values and %d changes (%v), want none", values, changes, err)
	}
}

// TestOrganisationsCantReachEachOthersSettings is the cross-organisation
// denial test for organisation settings: a member of one organisation can't
// read, change, reset or list the history of another's, and gets the same
// 404 as for an organisation that doesn't exist.
func TestOrganisationsCantReachEachOthersSettings(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	ada, _ := signIn(t, a, "ada@example.com", "")
	carol, _ := signIn(t, a, "carol@example.com", "")
	adaOrg, carolOrg := personalWorkspace(t, h, ada), personalWorkspace(t, h, carol)

	path := "/v1/orgs/" + adaOrg + "/settings/orgs.invitation_ttl"
	if r := do(t, h, "PUT", path, `{"value":"48h","version":0,"reason":"mine"}`, ada...); r.code != http.StatusOK {
		t.Fatalf("PUT %s as the owner = %d %s", path, r.code, r.body)
	}
	for _, r := range []struct{ method, path, body string }{
		{"GET", "/v1/orgs/" + adaOrg + "/settings", ""},
		{"GET", path, ""},
		{"PUT", path, `{"value":"72h","version":1,"reason":"theirs"}`},
		{"DELETE", path, `{"version":1,"reason":"theirs"}`},
		{"GET", path + "/history", ""},
	} {
		if got := do(t, h, r.method, r.path, r.body, carol...); got.code != http.StatusNotFound || got.json["code"] != "org_not_found" {
			t.Errorf("carol: %s %s = %d %s, want 404 org_not_found", r.method, r.path, got.code, got.body)
		}
	}

	// Carol's own organisation doesn't see Ada's value.
	own := do(t, h, "GET", "/v1/orgs/"+carolOrg+"/settings/orgs.invitation_ttl", "", carol...)
	if own.code != http.StatusOK || own.json["value"] != "168h0m0s" || own.json["overridden"] != false {
		t.Errorf("carol's own setting = %d %s, want the platform value", own.code, own.body)
	}
	if r := do(t, h, "GET", path, "", ada...); r.json["value"] != "48h0m0s" || r.json["version"] != float64(1) {
		t.Errorf("ada's setting after carol's attempts = %s, want unchanged", r.body)
	}
}
