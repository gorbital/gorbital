package app_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

var invitationToken = regexp.MustCompile(`#token=([A-Za-z0-9_-]+)`)

// emailedInvitation returns the token in the newest invitation email queued
// for to.
func emailedInvitation(t *testing.T, pool *pgxpool.Pool, to string) string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT args FROM river_job WHERE kind = 'apistock.mail.send' ORDER BY id DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var args struct {
			Message struct {
				To   []struct{ Email string } `json:"to"`
				Text string                   `json:"text"`
			} `json:"message"`
		}
		if json.Unmarshal(raw, &args) != nil || len(args.Message.To) == 0 || args.Message.To[0].Email != to {
			continue
		}
		if m := invitationToken.FindStringSubmatch(args.Message.Text); m != nil {
			return m[1]
		}
	}
	t.Fatalf("no invitation email for %s", to)
	return ""
}

// TestOrganisationsEndToEnd follows ADR-0048 over HTTP: create an
// organisation, invite someone, accept only with the invited address, keep
// non-members out, respect roles, and refuse to delete the account of an
// organisation's only owner.
func TestOrganisationsEndToEnd(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
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
	token := emailedInvitation(t, pool, "bob@example.com")
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
