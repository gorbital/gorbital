package orgshttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	orgslib "gorbital.dev/modules/orgs"
)

// The tests in this file prove the threat model of ADR-0083's Phase 7 notes.

// TestEveryOrganisationRouteRefusesOtherOrganisations calls every operation
// of the app whose path has {orgId} (the organisations module's, sign-in's
// service accounts and an app module guarded by guard.OrgMember) with an
// organisation ID the caller doesn't belong to, as a member of another
// organisation, as another organisation's service account and as an API
// key: each gets exactly the answer it gets for an organisation that doesn't
// exist or a malformed ID (404 org_not_found for a session; operations that
// need a session refuse keys before looking at the organisation), and
// nothing in the target organisation changes. The operations are read from the OpenAPI document, so a route
// added later without a check fails here.
func TestEveryOrganisationRouteRefusesOtherOrganisations(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	pool := newPool(t, dbURL)
	ada, adaID := signIn(t, a, "ada@example.com", "")
	carol, _ := signIn(t, a, "carol@example.com", "")
	bob, _ := signIn(t, a, "bob@example.com", "")
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	// Ada's organisation, with something behind every path parameter.
	target := newOrg(t, h, "Road Runners", ada)
	join(t, a, target, "bob@example.com", "member", ada, bob)
	base := "/v1/orgs/" + target
	invited := do(t, h, "POST", base+"/invitations", `{"email":"dave@example.com"}`, ada...)
	project := do(t, h, "POST", base+"/projects", `{"name":"Apollo"}`, ada...)
	account := do(t, h, "POST", base+"/service-accounts", `{"name":"Importer","role":"member"}`, ada...)
	accountID, _ := account.json["id"].(string)
	_, keyID := createKey(t, h, base+"/service-accounts/"+accountID+"/keys", fmt.Sprintf(`{"name":"import","expires_at":%q,"password":%q}`, expires, testPassword), ada)
	if invited.code != http.StatusCreated || project.code != http.StatusCreated || account.code != http.StatusCreated {
		t.Fatalf("setting up: invitation %d, project %d, service account %d", invited.code, project.code, account.code)
	}
	params := map[string]string{
		"userId": adaID, "invitationId": invited.json["id"].(string), "key": "orgs.invitation_ttl", "keyId": keyID,
	}

	// Callers from elsewhere: Carol's own organisation's owner, her
	// unscoped API key, and a service account of her organisation.
	carolOrg := newOrg(t, h, "Coyotes", carol)
	carolAccount := do(t, h, "POST", "/v1/orgs/"+carolOrg+"/service-accounts", `{"name":"Robot","role":"admin"}`, carol...)
	carolAccountID, _ := carolAccount.json["id"].(string)
	robotKey, _ := createKey(t, h, "/v1/orgs/"+carolOrg+"/service-accounts/"+carolAccountID+"/keys", fmt.Sprintf(`{"name":"robot","expires_at":%q,"password":%q}`, expires, testPassword), carol)
	carolKey, _ := createKey(t, h, "/v1/auth/api-keys", fmt.Sprintf(`{"name":"all","expires_at":%q,"password":%q}`, expires, testPassword), carol)
	callers := map[string][]string{
		"another organisation's owner":           carol,
		"another organisation's owner's API key": {"Authorization", "Bearer " + carolKey},
		"another organisation's service account": {"Authorization", "Bearer " + robotKey},
	}

	bodies := map[string]string{
		"orgs-rename":                                     `{"name":"Mine now","version":1}`,
		"orgs-members-change-role":                        `{"role":"owner"}`,
		"orgs-invitations-create":                         `{"email":"mallory@example.com","role":"owner"}`,
		"orgs-settings-set":                               `{"value":"48h","version":0,"reason":"x"}`,
		"orgs-settings-reset":                             `{"version":0,"reason":"x"}`,
		"orgs-create-service-account":                     `{"name":"Backdoor","role":"admin"}`,
		"orgs-update-service-account":                     `{"role":"admin"}`,
		"orgs-create-service-account-key":                 fmt.Sprintf(`{"name":"stolen","expires_at":%q,"password":%q}`, expires, testPassword),
		"projects-post-v1-orgs-by-org-id-projects":        `{"name":"Intruder"}`,
		"projects-patch-v1-orgs-by-org-id-projects-by-id": `{"name":"Renamed","version":1}`,
	}

	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	spec := do(t, h, "GET", "/openapi.json", "")
	if err := json.Unmarshal([]byte(spec.body), &doc); err != nil {
		t.Fatalf("GET /openapi.json = %d: %v", spec.code, err)
	}
	tested := 0
	for path, methods := range doc.Paths {
		if !strings.Contains(path, "{orgId}") {
			continue
		}
		for method, op := range methods {
			method = strings.ToUpper(method)
			concrete := func(org string) string {
				p := strings.ReplaceAll(path, "{orgId}", org)
				id := project.json["id"].(string)
				if strings.Contains(path, "/service-accounts/") {
					id = accountID
				}
				p = strings.ReplaceAll(p, "{id}", id)
				for name, value := range params {
					p = strings.ReplaceAll(p, "{"+name+"}", value)
				}
				return p
			}
			body := bodies[op.OperationID]
			tested++
			// A caller learns nothing about an organisation it isn't in: the
			// answer is the one it gets for an organisation that doesn't
			// exist, which for a member's session is 404 org_not_found.
			for who, headers := range callers {
				missing := do(t, h, method, concrete(string(orgslib.NewID())), body, headers...)
				malformed := do(t, h, method, concrete("org_nope"), body, headers...)
				r := do(t, h, method, concrete(target), body, headers...)
				for _, other := range []response{malformed, r} {
					if other.code != missing.code || other.json["code"] != missing.json["code"] || other.json["detail"] != missing.json["detail"] {
						t.Errorf("%s %s (%s) as %s = %d %s, want the answer for an unknown organisation: %d %s", method, path, op.OperationID, who, other.code, other.body, missing.code, missing.body)
					}
				}
				if who == "another organisation's owner" && (r.code != http.StatusNotFound || r.json["code"] != "org_not_found") {
					t.Errorf("%s %s (%s) as %s = %d %s, want 404 org_not_found", method, path, op.OperationID, who, r.code, r.body)
				}
				if r.code < 400 {
					t.Errorf("%s %s (%s) as %s = %d %s, want a refusal", method, path, op.OperationID, who, r.code, r.body)
				}
			}
		}
	}
	if tested < 30 {
		t.Errorf("tested %d organisation operations, want every one of orgs, service accounts, settings, flags and projects", tested)
	}

	// Nothing changed in Ada's organisation.
	var name string
	var deleted bool
	var members, invitations, projects, accounts, keys int
	err := pool.QueryRow(context.Background(), `SELECT name, deleted_at IS NOT NULL,
		(SELECT count(*) FROM org_members WHERE org_id = $1),
		(SELECT count(*) FROM org_invitations WHERE org_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL),
		(SELECT count(*) FROM projects WHERE org_id = $1),
		(SELECT count(*) FROM auth_service_accounts WHERE org_id = $1 AND disabled_at IS NULL),
		(SELECT count(*) FROM auth_api_keys WHERE service_account_id = $2 AND revoked_at IS NULL)
		FROM orgs WHERE id = $1`, target, accountID).Scan(&name, &deleted, &members, &invitations, &projects, &accounts, &keys)
	if err != nil || name != "Road Runners" || deleted || members != 2 || invitations != 1 || projects != 1 || accounts != 1 || keys != 1 {
		t.Errorf("target organisation after the attempts: %q deleted=%v members=%d invitations=%d projects=%d accounts=%d keys=%d (%v); want it unchanged",
			name, deleted, members, invitations, projects, accounts, keys, err)
	}
	var overridden int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM settings_values WHERE org_id = $1`, target).Scan(&overridden); err != nil || overridden != 0 {
		t.Errorf("target organisation's settings = %d, %v; want none", overridden, err)
	}
}

// TestInvitationTokens checks how invitation tokens are handled: only a
// hash is stored, a token works once, for the invited verified address,
// while pending and while its inviter may still give the role; and every
// token that doesn't work (unknown, used, revoked, expired, replaced by a
// resend) gets the same answer.
func TestInvitationTokens(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	pool := newPool(t, dbURL)
	ctx := context.Background()
	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, _ := signIn(t, a, "bob@example.com", "")
	carol, _ := signIn(t, a, "carol@example.com", "")
	org := newOrg(t, h, "Road Runners", ada)
	base := "/v1/orgs/" + org
	accept := func(token string, as []string) response {
		return do(t, h, "POST", "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, token), as...)
	}
	invite := func(email string) (id, token string) {
		t.Helper()
		r := do(t, h, "POST", base+"/invitations", fmt.Sprintf(`{"email":%q}`, email), ada...)
		if r.code != http.StatusCreated || r.json["token"] != nil || strings.Contains(r.body, "token") {
			t.Fatalf("invite %s = %d %s, want 201 without the token", email, r.code, r.body)
		}
		return r.json["id"].(string), emailedInvitation(t, a, email)
	}

	// A token has at least 256 bits of entropy's worth of characters and is
	// stored only as a hash, never in audit events.
	_, token := invite("bob@example.com")
	if len(token) < 43 {
		t.Errorf("token %q is %d characters, want at least 43 (256 bits)", token, len(token))
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT (SELECT string_agg(i::text, ' ') FROM org_invitations i) || (SELECT string_agg(e::text, ' ') FROM audit_events e)`).Scan(&stored); err != nil || strings.Contains(stored, token) {
		t.Errorf("the invitation token is stored in org_invitations or audit_events (%v)", err)
	}

	// Email binding: a forwarded link is refused, and stays usable by the
	// invited address.
	if r := accept(token, carol); r.code != http.StatusForbidden || r.json["code"] != "invitation_for_another_email" {
		t.Errorf("accept with another address = %d %s, want 403 invitation_for_another_email", r.code, r.body)
	}
	if r := accept(token, bob); r.code != http.StatusOK {
		t.Fatalf("accept = %d %s", r.code, r.body)
	}

	// Every token that doesn't work answers the same.
	unknown := accept(strings.Repeat("A", len(token)), carol)
	if unknown.code != http.StatusNotFound || unknown.json["code"] != "invitation_not_found" {
		t.Fatalf("unknown token = %d %s", unknown.code, unknown.body)
	}
	same := func(name string, r response) {
		t.Helper()
		if r.code != unknown.code || r.json["code"] != unknown.json["code"] || r.json["detail"] != unknown.json["detail"] {
			t.Errorf("%s = %d %s, want the answer to an unknown token", name, r.code, r.body)
		}
	}
	same("a used token", accept(token, bob))

	revokedID, revokedToken := invite("carol@example.com")
	if r := do(t, h, "DELETE", base+"/invitations/"+revokedID, "", ada...); r.code != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", r.code, r.body)
	}
	same("a revoked token", accept(revokedToken, carol))

	resentID, oldToken := invite("carol@example.com")
	if r := do(t, h, "POST", base+"/invitations/"+resentID+"/resend", "", ada...); r.code != http.StatusOK {
		t.Fatalf("resend = %d %s", r.code, r.body)
	}
	newToken := emailedInvitation(t, a, "carol@example.com")
	if newToken == oldToken {
		t.Fatal("resending kept the token")
	}
	same("the token a resend replaced", accept(oldToken, carol))

	if _, err := pool.Exec(ctx, `UPDATE org_invitations SET expires_at = now() - interval '1 second' WHERE id = $1`, resentID); err != nil {
		t.Fatal(err)
	}
	same("an expired token", accept(newToken, carol))
	same("an empty token", accept("  ", carol))

	// An invitation is only as good as its inviter: once the admin who sent
	// it can't give the role any more, it doesn't work.
	join(t, a, org, "dave@example.com", "admin", ada, mustSignIn(t, a, "dave@example.com"))
	dave := signInAgain(t, h, "dave@example.com")
	r := do(t, h, "POST", base+"/invitations", `{"email":"erin@example.com","role":"admin"}`, dave...)
	if r.code != http.StatusCreated {
		t.Fatalf("admin invites = %d %s", r.code, r.body)
	}
	erinToken := emailedInvitation(t, a, "erin@example.com")
	erin, _ := signIn(t, a, "erin@example.com", "")
	daveID := memberID(t, h, org, "dave@example.com", ada)
	if r := do(t, h, "PATCH", base+"/members/"+daveID, `{"role":"member"}`, ada...); r.code != http.StatusOK {
		t.Fatalf("demote the inviter = %d %s", r.code, r.body)
	}
	if r := accept(erinToken, erin); r.code == http.StatusOK {
		t.Errorf("accept an invitation whose inviter was demoted = %d %s, want a refusal", r.code, r.body)
	}
}

// mustSignIn signs up email and returns its session header.
func mustSignIn(t *testing.T, a *testApp, email string) []string {
	t.Helper()
	h, _ := signIn(t, a, email, "")
	return h
}

// signInAgain signs an existing account in with a new bearer token.
func signInAgain(t *testing.T, h http.Handler, email string) []string {
	t.Helper()
	r := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q,"transport":"bearer"}`, email, testPassword))
	token, _ := r.json["token"].(string)
	if r.code != http.StatusOK || token == "" {
		t.Fatalf("login %s = %d %s", email, r.code, r.body)
	}
	return []string{"Authorization", "Bearer " + token}
}

// memberID returns the user ID of org's member with email.
func memberID(t *testing.T, h http.Handler, org, email string, as []string) string {
	t.Helper()
	r := do(t, h, "GET", "/v1/orgs/"+org+"/members", "", as...)
	items, _ := r.json["items"].([]any)
	for _, item := range items {
		if m := item.(map[string]any); m["email"] == email {
			return m["user_id"].(string)
		}
	}
	t.Fatalf("GET members = %d %s, no %s", r.code, r.body, email)
	return ""
}

// TestRoleEscalation checks the role_not_allowed rules over HTTP: nobody
// gives, changes or removes a role above their own, only owners manage
// owners, roles an app adds are compared by their permissions, and the same
// holds for service accounts.
func TestRoleEscalation(t *testing.T) {
	// billing is a role an app module adds, with a permission admins don't
	// hold.
	billing := gorbital.Module{
		Name: "billing",
		Permissions: []gorbital.Permission{
			{Name: "billing.invoice.void", Description: "Void invoices", OrgRoles: []string{orgslib.RoleOwner, "billing"}},
			{Name: "billing.invoice.read", Description: "See invoices", OrgRoles: []string{orgslib.RoleOwner, orgslib.RoleAdmin, orgslib.RoleMember, "billing"}},
		},
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			gorbital.Delete(r, "/v1/orgs/{orgId}/invoices", func(context.Context, *struct {
				OrgID string `path:"orgId"`
			}) (*struct{}, error) {
				return nil, nil
			}, gorbital.Status(http.StatusNoContent), guard.OrgMember("billing.invoice.void"))
		},
	}
	a := newApp(t, nil, billing)
	h := a.Handler()
	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, bobID := signIn(t, a, "bob@example.com", "")
	carol, carolID := signIn(t, a, "carol@example.com", "")
	org := newOrg(t, h, "Road Runners", ada)
	base := "/v1/orgs/" + org
	join(t, a, org, "bob@example.com", "admin", ada, bob)
	join(t, a, org, "carol@example.com", "member", ada, carol)
	adaID := memberID(t, h, org, "ada@example.com", ada)

	for _, tt := range []struct {
		name, method, path, body string
		as                       []string
		code                     int
		errCode                  string
	}{
		{"an admin invites an owner", "POST", base + "/invitations", `{"email":"x@example.com","role":"owner"}`, bob, 403, "role_not_allowed"},
		{"an admin invites a role with more permissions", "POST", base + "/invitations", `{"email":"x@example.com","role":"billing"}`, bob, 403, "role_not_allowed"},
		{"an admin makes themselves an owner", "PATCH", base + "/members/" + bobID, `{"role":"owner"}`, bob, 403, "role_not_allowed"},
		{"an admin gives a member a role with more permissions", "PATCH", base + "/members/" + carolID, `{"role":"billing"}`, bob, 403, "role_not_allowed"},
		{"an admin demotes an owner", "PATCH", base + "/members/" + adaID, `{"role":"member"}`, bob, 403, "role_not_allowed"},
		{"an admin removes an owner", "DELETE", base + "/members/" + adaID, "", bob, 403, "role_not_allowed"},
		{"a member changes a role", "PATCH", base + "/members/" + carolID, `{"role":"admin"}`, carol, 403, "forbidden"},
		{"a member invites", "POST", base + "/invitations", `{"email":"x@example.com"}`, carol, 403, "forbidden"},
		{"a role the organisation doesn't have", "PATCH", base + "/members/" + carolID, `{"role":"platform_admin"}`, ada, 422, "unknown_role"},
		{"an admin creates a service account above their role", "POST", base + "/service-accounts", `{"name":"x","role":"billing"}`, bob, 422, "invalid_service_account_role"},
		{"an owner creates an owner service account", "POST", base + "/service-accounts", `{"name":"x","role":"owner"}`, ada, 422, "invalid_service_account_role"},
		{"an admin reaches the role's route", "DELETE", base + "/invoices", "", bob, 403, "forbidden"},
	} {
		if r := do(t, h, tt.method, tt.path, tt.body, tt.as...); r.code != tt.code || r.json["code"] != tt.errCode {
			t.Errorf("%s: %s %s = %d %s, want %d %s", tt.name, tt.method, tt.path, r.code, r.body, tt.code, tt.errCode)
		}
	}
	// The owner may; then the role reaches its route.
	if r := do(t, h, "PATCH", base+"/members/"+carolID, `{"role":"billing"}`, ada...); r.code != http.StatusOK {
		t.Fatalf("owner gives billing = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", base+"/invoices", "", carol...); r.code != http.StatusNoContent {
		t.Errorf("billing member voids = %d %s", r.code, r.body)
	}
	// An organisation role never becomes a platform role.
	if r := do(t, h, "GET", "/ops/settings", "", ada...); r.code != http.StatusForbidden {
		t.Errorf("an organisation owner reads /ops = %d %s, want 403", r.code, r.body)
	}
}

// TestServiceAccountKeyScopes checks that a service account's key holds at
// most its role's permissions, narrowed by its scopes, in its organisation
// only.
func TestServiceAccountKeyScopes(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	ada, _ := signIn(t, a, "ada@example.com", "")
	org := newOrg(t, h, "Road Runners", ada)
	accounts := "/v1/orgs/" + org + "/service-accounts"
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	created := do(t, h, "POST", accounts, `{"name":"Reader","role":"member"}`, ada...)
	id, _ := created.json["id"].(string)
	if created.code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.code, created.body)
	}
	keys := accounts + "/" + id + "/keys"
	if r := do(t, h, "POST", keys, fmt.Sprintf(`{"name":"x","expires_at":%q,"password":%q,"scopes":["orgs.org.delete"]}`, expires, testPassword), ada...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "invalid_api_key_scopes" {
		t.Errorf("a key scoped beyond the role = %d %s, want 422 invalid_api_key_scopes", r.code, r.body)
	}
	readKey, _ := createKey(t, h, keys, fmt.Sprintf(`{"name":"read","expires_at":%q,"password":%q,"scopes":["projects.project.read"]}`, expires, testPassword), ada)
	reader := []string{"Authorization", "Bearer " + readKey}
	projects := "/v1/orgs/" + org + "/projects"
	if r := do(t, h, "GET", projects, "", reader...); r.code != http.StatusOK {
		t.Errorf("read with the read key = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", projects, `{"name":"Nope"}`, reader...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("write with the read key = %d %s, want 403 forbidden", r.code, r.body)
	}
	personal := personalWorkspace(t, h, ada)
	if r := do(t, h, "GET", "/v1/orgs/"+personal+"/projects", "", reader...); r.code != http.StatusNotFound || r.json["code"] != "org_not_found" {
		t.Errorf("the key in its creator's other organisation = %d %s, want 404 org_not_found", r.code, r.body)
	}
}
