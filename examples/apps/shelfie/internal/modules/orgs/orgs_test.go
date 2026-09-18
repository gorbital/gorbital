package orgshttp

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"
)

// TestOrganisationsEndToEnd follows ADR-0048 over HTTP: create an
// organisation, invite someone, accept only with the invited address, keep
// non-members out, respect roles, and refuse to delete the account of an
// organisation's only owner.
func TestOrganisationsEndToEnd(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	pool := newPool(t, dbURL)
	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, bobID := signIn(t, a, "bob@example.com", "")
	carol, _ := signIn(t, a, "carol@example.com", "")

	if r := do(t, h, "GET", "/v1/orgs", ""); r.code != http.StatusUnauthorized {
		t.Errorf("list without signing in = %d %s", r.code, r.body)
	}
	personal := personalWorkspace(t, h, ada)
	if r := do(t, h, "POST", "/v1/orgs/"+personal+"/invitations", `{"email":"bob@example.com"}`, ada...); r.code != http.StatusConflict || r.json["code"] != "personal_workspace" {
		t.Errorf("invite into a personal workspace = %d %s, want 409 personal_workspace", r.code, r.body)
	}

	created := do(t, h, "POST", "/v1/orgs", `{"name":"Road Runners"}`, ada...)
	if created.code != http.StatusCreated || created.json["role"] != "owner" || created.json["personal"] != false {
		t.Fatalf("create = %d %s", created.code, created.body)
	}
	org, _ := created.json["id"].(string)
	base := "/v1/orgs/" + org

	invited := do(t, h, "POST", base+"/invitations", `{"email":"bob@example.com","role":"admin"}`, ada...)
	if invited.code != http.StatusCreated || invited.json["role"] != "admin" || invited.json["token"] != nil {
		t.Fatalf("invite = %d %s, want 201 without the token", invited.code, invited.body)
	}
	token := emailedInvitation(t, a, "bob@example.com")
	accept := fmt.Sprintf(`{"token":%q}`, token)

	// A forwarded link is no use to anyone else; non-members don't see the
	// organisation.
	if r := do(t, h, "POST", "/v1/invitations/accept", accept, carol...); r.code != http.StatusForbidden || r.json["code"] != "invitation_for_another_email" {
		t.Errorf("accept with another address = %d %s", r.code, r.body)
	}
	for _, r := range []response{
		do(t, h, "GET", base, "", carol...),
		do(t, h, "GET", base+"/members", "", carol...),
		do(t, h, "POST", base+"/invitations", `{"email":"carol@example.com"}`, carol...),
		do(t, h, "GET", "/v1/orgs/org_doesnotexistatallxxxxxxxx", "", carol...),
	} {
		if r.code != http.StatusNotFound || r.json["code"] != "org_not_found" {
			t.Errorf("non-member request = %d %s, want 404 org_not_found", r.code, r.body)
		}
	}

	joined := do(t, h, "POST", "/v1/invitations/accept", accept, bob...)
	if joined.code != http.StatusOK || joined.json["id"] != org || joined.json["role"] != "admin" {
		t.Fatalf("accept = %d %s", joined.code, joined.body)
	}
	if r := do(t, h, "POST", "/v1/invitations/accept", accept, bob...); r.code != http.StatusNotFound || r.json["code"] != "invitation_not_found" {
		t.Errorf("accept again = %d %s, want 404 invitation_not_found", r.code, r.body)
	}
	members := do(t, h, "GET", base+"/members", "", bob...)
	if items, _ := members.json["items"].([]any); members.code != http.StatusOK || len(items) != 2 || items[0].(map[string]any)["role"] != "owner" {
		t.Errorf("members = %d %s, want the owner first and bob", members.code, members.body)
	}

	// An admin can't touch owners or delete the organisation.
	if r := do(t, h, "POST", base+"/invitations", `{"email":"carol@example.com","role":"owner"}`, bob...); r.code != http.StatusForbidden || r.json["code"] != "role_not_allowed" {
		t.Errorf("admin invites an owner = %d %s, want 403 role_not_allowed", r.code, r.body)
	}
	if r := do(t, h, "DELETE", base, "", bob...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("admin deletes the organisation = %d %s, want 403 forbidden", r.code, r.body)
	}
	if r := do(t, h, "POST", base+"/leave", "", ada...); r.code != http.StatusConflict || r.json["code"] != "last_owner" {
		t.Errorf("last owner leaves = %d %s, want 409 last_owner", r.code, r.body)
	}

	// The only owner can't delete their account until someone else owns it.
	blocked := do(t, h, "DELETE", "/v1/auth/me", fmt.Sprintf(`{"password":%q}`, testPassword), ada...)
	listed, _ := blocked.json["errors"].([]any)
	if blocked.code != http.StatusConflict || blocked.json["code"] != "sole_owner" || len(listed) != 1 || listed[0].(map[string]any)["message"] != org {
		t.Fatalf("delete the only owner's account = %d %s, want 409 sole_owner listing %s", blocked.code, blocked.body, org)
	}
	if r := do(t, h, "PATCH", base+"/members/"+bobID, `{"role":"owner"}`, ada...); r.code != http.StatusOK || r.json["role"] != "owner" {
		t.Fatalf("make bob an owner = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/auth/me", fmt.Sprintf(`{"password":%q}`, testPassword), ada...); r.code != http.StatusNoContent {
		t.Fatalf("delete ada's account = %d %s", r.code, r.body)
	}
	members = do(t, h, "GET", base+"/members", "", bob...)
	if items, _ := members.json["items"].([]any); members.code != http.StatusOK || len(items) != 1 {
		t.Errorf("members after ada's account was deleted = %d %s, want bob only", members.code, members.body)
	}

	rows, err := pool.Query(context.Background(), `SELECT action FROM audit_events WHERE org_id = $1 ORDER BY id`, org)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		actions = append(actions, action)
	}
	for _, want := range []string{"orgs.org.created", "orgs.invitation.created", "orgs.invitation.accepted", "orgs.member.added", "orgs.member.role_changed", "orgs.member.removed"} {
		if !slices.Contains(actions, want) {
			t.Errorf("audit actions for the organisation %v lack %s", actions, want)
		}
	}
}

// TestAPIKeysAndOrganisations checks what API keys may do with
// organisations (ADR-0058): creating and listing them needs a scope, like
// the organisation permissions do, and joining or leaving one needs the
// person's session.
func TestAPIKeysAndOrganisations(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, bobID := signIn(t, a, "bob@example.com", "")
	expires := time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	key := func(session []string, scopes string) []string {
		k, _ := createKey(t, h, "/v1/auth/api-keys", fmt.Sprintf(`{"name":"CI","expires_at":%q,"password":%q,"scopes":%s}`, expires, testPassword, scopes), session)
		return []string{"Authorization", "Bearer " + k}
	}

	projectsOnly := key(ada, `["projects.project.read"]`)
	for _, req := range []struct{ method, path, body string }{
		{"GET", "/v1/orgs", ""},
		{"POST", "/v1/orgs", `{"name":"By a key"}`},
	} {
		if r := do(t, h, req.method, req.path, req.body, projectsOnly...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
			t.Errorf("%s %s with a projects-only key = %d %s, want 403 forbidden", req.method, req.path, r.code, r.body)
		}
	}
	orgsKey := key(ada, `["orgs.org.create","orgs.org.list","orgs.org.read"]`)
	created := do(t, h, "POST", "/v1/orgs", `{"name":"Road Runners"}`, orgsKey...)
	if created.code != http.StatusCreated {
		t.Fatalf("create with orgs.org.create = %d %s", created.code, created.body)
	}
	base := "/v1/orgs/" + created.json["id"].(string)
	if r := do(t, h, "GET", "/v1/orgs", "", orgsKey...); r.code != http.StatusOK || len(r.json["items"].([]any)) != 2 {
		t.Errorf("list with orgs.org.list = %d %s, want both organisations", r.code, r.body)
	}
	// Organisation permissions are limited by the same scopes.
	if r := do(t, h, "GET", base, "", orgsKey...); r.code != http.StatusOK {
		t.Errorf("get with orgs.org.read = %d %s", r.code, r.body)
	}
	for _, req := range []struct{ method, path, body string }{
		{"PATCH", base, `{"name":"Renamed","version":1}`},
		{"GET", base + "/members", ""},
		{"POST", base + "/invitations", `{"email":"bob@example.com"}`},
		{"GET", projectsOf(t, h, ada), ""},
	} {
		if r := do(t, h, req.method, req.path, req.body, orgsKey...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
			t.Errorf("%s %s outside the key's scopes = %d %s, want 403 forbidden", req.method, req.path, r.code, r.body)
		}
	}

	// Joining and leaving need a session, even for an unscoped key.
	if r := do(t, h, "POST", base+"/invitations", `{"email":"bob@example.com"}`, ada...); r.code != http.StatusCreated {
		t.Fatalf("invite = %d %s", r.code, r.body)
	}
	accept := fmt.Sprintf(`{"token":%q}`, emailedInvitation(t, a, "bob@example.com"))
	bobKey := key(bob, `[]`)
	if r := do(t, h, "POST", "/v1/invitations/accept", accept, bobKey...); r.code != http.StatusForbidden || r.json["code"] != "session_required" {
		t.Errorf("accept with a key = %d %s, want 403 session_required", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/invitations/accept", accept, bob...); r.code != http.StatusOK {
		t.Fatalf("accept with the session = %d %s", r.code, r.body)
	}
	for _, req := range []struct{ method, path string }{
		{"POST", base + "/leave"},
		{"DELETE", base + "/members/" + bobID},
	} {
		if r := do(t, h, req.method, req.path, "", bobKey...); r.code != http.StatusForbidden || r.json["code"] != "session_required" {
			t.Errorf("%s %s with a key = %d %s, want 403 session_required", req.method, req.path, r.code, r.body)
		}
	}
	if r := do(t, h, "GET", base, "", bobKey...); r.code != http.StatusOK {
		t.Errorf("the member's unscoped key in the organisation = %d %s, want 200", r.code, r.body)
	}
	if r := do(t, h, "POST", base+"/leave", "", bob...); r.code != http.StatusNoContent {
		t.Errorf("leave with the session = %d %s", r.code, r.body)
	}
}

// TestIdempotencyKeysStayInTheirOrganisation sends one key with the same
// body to two organisations of the same member: the second is refused rather
// than replaying the first organisation's project (ADR-0060).
func TestIdempotencyKeysStayInTheirOrganisation(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	ada, _ := signIn(t, a, "ada@example.com", "")
	personal := projectsOf(t, h, ada)
	team := do(t, h, "POST", "/v1/orgs", `{"name":"Road Runners"}`, ada...)
	if team.code != http.StatusCreated {
		t.Fatalf("create organisation = %d %s", team.code, team.body)
	}
	teamProjects := "/v1/orgs/" + team.json["id"].(string) + "/projects"

	body := `{"name":"Website"}`
	if r := do(t, h, "POST", personal, body, withKey(ada, "website")...); r.code != http.StatusCreated {
		t.Fatalf("create in the personal workspace = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", teamProjects, body, withKey(ada, "website")...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "idempotency_key_reused" {
		t.Errorf("same key in another organisation = %d %s, want 422 idempotency_key_reused", r.code, r.body)
	}
	if items, _ := do(t, h, "GET", teamProjects, "", ada...).json["items"].([]any); len(items) != 0 {
		t.Errorf("the other organisation has %d projects, want none", len(items))
	}
}
