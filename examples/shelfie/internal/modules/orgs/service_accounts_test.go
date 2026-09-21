package orgshttp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestOrganisationsCantReachEachOthersServiceAccounts follows ADR-0058 in a
// multi-tenant app: an organisation's owners and admins manage its service
// accounts; their keys act only in that organisation, with its role and
// never as a member managing it; nobody outside the organisation can see
// them, and personal keys keep their scopes in organisations.
func TestOrganisationsCantReachEachOthersServiceAccounts(t *testing.T) {
	a, dbURL := newAppWithURL(t, nil)
	h := a.Handler()
	pool := newPool(t, dbURL)
	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, _ := signIn(t, a, "bob@example.com", "")
	carol, _ := signIn(t, a, "carol@example.com", "")
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	newOrg := func(name string, owner []string) string {
		r := do(t, h, "POST", "/v1/orgs", fmt.Sprintf(`{"name":%q}`, name), owner...)
		id, _ := r.json["id"].(string)
		if r.code != http.StatusCreated {
			t.Fatalf("create %s = %d %s", name, r.code, r.body)
		}
		return id
	}
	orgA, orgB := newOrg("Road Runners", ada), newOrg("Coyotes", carol)
	if r := do(t, h, "POST", "/v1/orgs/"+orgA+"/invitations", `{"email":"bob@example.com","role":"member"}`, ada...); r.code != http.StatusCreated {
		t.Fatalf("invite = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, emailedInvitation(t, a, "bob@example.com")), bob...); r.code != http.StatusOK {
		t.Fatalf("accept = %d %s", r.code, r.body)
	}
	baseA, baseB := "/v1/orgs/"+orgA+"/service-accounts", "/v1/orgs/"+orgB+"/service-accounts"

	for _, role := range []string{"owner", "nope"} {
		if r := do(t, h, "POST", baseA, fmt.Sprintf(`{"name":"robot","role":%q}`, role), ada...); r.code != http.StatusUnprocessableEntity || r.json["code"] != "invalid_service_account_role" {
			t.Errorf("create with role %s = %d %s, want 422 invalid_service_account_role", role, r.code, r.body)
		}
	}
	if r := do(t, h, "POST", baseA, `{"name":"robot","role":"member"}`, bob...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("create as a member = %d %s, want 403 forbidden", r.code, r.body)
	}
	created := do(t, h, "POST", baseA, `{"name":"Importer","role":"member"}`, ada...)
	id, _ := created.json["id"].(string)
	if created.code != http.StatusCreated || created.json["org_id"] != orgA || created.json["role"] != "member" {
		t.Fatalf("create = %d %s", created.code, created.body)
	}
	item := baseA + "/" + id
	key, _ := createKey(t, h, item+"/keys", fmt.Sprintf(`{"name":"import","expires_at":%q,"password":%q}`, expires, testPassword), ada)
	bearer := []string{"Authorization", "Bearer " + key}

	// Nobody outside the organisation reaches its service accounts, through
	// its routes or their own.
	for _, r := range []response{
		do(t, h, "GET", baseA, "", carol...),
		do(t, h, "GET", item, "", carol...),
		do(t, h, "PATCH", item, `{"disabled":true}`, carol...),
		do(t, h, "DELETE", item, "", carol...),
		do(t, h, "GET", item+"/keys", "", carol...),
		do(t, h, "POST", item+"/keys", fmt.Sprintf(`{"name":"mine","expires_at":%q,"password":%q}`, expires, testPassword), carol...),
	} {
		if r.code != http.StatusNotFound || r.json["code"] != "org_not_found" {
			t.Errorf("another organisation's owner = %d %s, want 404 org_not_found", r.code, r.body)
		}
	}
	for _, r := range []response{
		do(t, h, "GET", baseB+"/"+id, "", carol...),
		do(t, h, "PATCH", baseB+"/"+id, `{"role":"admin"}`, carol...),
		do(t, h, "DELETE", baseB+"/"+id, "", carol...),
		do(t, h, "GET", baseB+"/"+id+"/keys", "", carol...),
		do(t, h, "POST", baseB+"/"+id+"/keys", fmt.Sprintf(`{"name":"mine","expires_at":%q,"password":%q}`, expires, testPassword), carol...),
	} {
		if r.code != http.StatusNotFound || r.json["code"] != "service_account_not_found" {
			t.Errorf("another organisation's service account through one's own = %d %s, want 404 service_account_not_found", r.code, r.body)
		}
	}
	if r := do(t, h, "GET", baseB, "", carol...); r.code != http.StatusOK || strings.Contains(r.body, id) {
		t.Errorf("list in another organisation = %d %s, want it absent", r.code, r.body)
	}
	operator, _ := signIn(t, a, "operator@example.com", "platform_admin")
	if r := do(t, h, "GET", "/ops/service-accounts/"+id, "", operator...); r.code != http.StatusNotFound {
		t.Errorf("an organisation's service account through /ops = %d %s, want 404", r.code, r.body)
	}

	// The key works in its organisation with its role.
	if r := do(t, h, "POST", "/v1/orgs/"+orgA+"/projects", `{"name":"Imported","description":"From the importer"}`, bearer...); r.code != http.StatusCreated {
		t.Errorf("create a project with the service account's key = %d %s, want 201", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/orgs/"+orgA+"/projects", "", bearer...); r.code != http.StatusOK || !strings.Contains(r.body, "Imported") {
		t.Errorf("list projects with the service account's key = %d %s", r.code, r.body)
	}
	var actorKind string
	if err := pool.QueryRow(context.Background(), `SELECT actor_kind FROM audit_events WHERE action = 'projects.project.created' AND org_id = $1`, orgA).Scan(&actorKind); err != nil || actorKind != "service" {
		t.Errorf("projects.project.created actor = %q, %v; want the service account", actorKind, err)
	}
	// ...and nowhere else, nor as a member managing the organisation.
	for _, req := range []struct{ method, path, body string }{
		{"GET", "/v1/orgs/" + orgB + "/projects", ""},
		{"POST", "/v1/orgs/" + orgB + "/projects", `{"name":"Intruder","description":"x"}`},
		{"GET", "/v1/orgs/" + orgA, ""},
		{"GET", "/v1/orgs/" + orgA + "/members", ""},
		{"POST", "/v1/orgs/" + orgA + "/invitations", `{"email":"mallory@example.com"}`},
		{"DELETE", "/v1/orgs/" + orgA, ""},
	} {
		if r := do(t, h, req.method, req.path, req.body, bearer...); r.code != http.StatusNotFound || r.json["code"] != "org_not_found" {
			t.Errorf("%s %s with the service account's key = %d %s, want 404 org_not_found", req.method, req.path, r.code, r.body)
		}
	}
	if r := do(t, h, "GET", baseA, "", bearer...); r.code != http.StatusForbidden || r.json["code"] != "session_required" {
		t.Errorf("list service accounts with a service account's key = %d %s, want 403 session_required", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/orgs", "", bearer...); r.code != http.StatusUnauthorized {
		t.Errorf("GET /v1/orgs with a service account's key = %d %s, want 401", r.code, r.body)
	}

	// A personal key keeps its scopes in organisations, and an admin's key
	// can't manage service accounts.
	readOnly, _ := createKey(t, h, "/v1/auth/api-keys", fmt.Sprintf(`{"name":"read","expires_at":%q,"password":%q,"scopes":["projects.project.read"]}`, expires, testPassword), ada)
	if r := do(t, h, "GET", "/v1/orgs/"+orgA+"/projects", "", "Authorization", "Bearer "+readOnly); r.code != http.StatusOK {
		t.Errorf("read projects with a read-only key = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/orgs/"+orgA+"/projects", `{"name":"Nope","description":"x"}`, "Authorization", "Bearer "+readOnly); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("create a project with a read-only key = %d %s, want 403 forbidden", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/orgs/"+orgA, "", "Authorization", "Bearer "+readOnly); r.code != http.StatusForbidden {
		t.Errorf("delete the organisation with a read-only key = %d %s, want 403", r.code, r.body)
	}
	ownerKey, _ := createKey(t, h, "/v1/auth/api-keys", fmt.Sprintf(`{"name":"all","expires_at":%q,"password":%q}`, expires, testPassword), ada)
	if r := do(t, h, "POST", baseA, `{"name":"robot","role":"member"}`, "Authorization", "Bearer "+ownerKey); r.code != http.StatusForbidden || r.json["code"] != "session_required" {
		t.Errorf("create a service account with the owner's key = %d %s, want 403 session_required", r.code, r.body)
	}

	// Disabling stops the key; deleting the organisation stops a new one;
	// purging it removes its service accounts.
	if r := do(t, h, "PATCH", item, `{"disabled":true}`, ada...); r.code != http.StatusOK || r.json["disabled"] != true {
		t.Fatalf("disable = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/orgs/"+orgA+"/projects", "", bearer...); r.code != http.StatusUnauthorized {
		t.Errorf("disabled service account's key = %d %s, want 401", r.code, r.body)
	}
	if r := do(t, h, "PATCH", item, `{"disabled":false}`, ada...); r.code != http.StatusOK {
		t.Fatalf("enable = %d %s", r.code, r.body)
	}
	fresh, _ := createKey(t, h, item+"/keys", fmt.Sprintf(`{"name":"again","expires_at":%q,"password":%q}`, expires, testPassword), ada)
	if r := do(t, h, "GET", "/v1/orgs/"+orgA+"/projects", "", "Authorization", "Bearer "+fresh); r.code != http.StatusOK {
		t.Errorf("new key after enabling = %d %s", r.code, r.body)
	}
	if r := do(t, h, "DELETE", "/v1/orgs/"+orgA, "", ada...); r.code != http.StatusNoContent {
		t.Fatalf("delete the organisation = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/orgs/"+orgA+"/projects", "", "Authorization", "Bearer "+fresh); r.code != http.StatusNotFound || r.json["code"] != "org_not_found" {
		t.Errorf("key of a deleted organisation's service account = %d %s, want 404 org_not_found", r.code, r.body)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE orgs SET purge_after = now() - interval '1 second' WHERE id = $1`, orgA); err != nil {
		t.Fatal(err)
	}
	if n, err := a.Orgs().Purge(context.Background()); err != nil || n != 1 {
		t.Fatalf("Purge() = %d, %v", n, err)
	}
	var accounts, keys int
	if err := pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM auth_service_accounts WHERE org_id = $1), (SELECT count(*) FROM auth_api_keys WHERE service_account_id = $2)`, orgA, id).Scan(&accounts, &keys); err != nil || accounts != 0 || keys != 0 {
		t.Errorf("after the purge: %d service accounts, %d keys, %v; want none", accounts, keys, err)
	}
	noSecretsStored(t, pool, key, readOnly, ownerKey, fresh)
}
