package orgshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/guard"
	orgslib "gorbital.dev/modules/orgs"
)

// The words of the app that mounts organisations as merchants.
var merchant = []Option{ScopeName("merchant", "merchants", "merchantId"), WithoutServiceAccounts()}

// permDashboardRead is the merchant app's own scope permission, held by
// every merchant role.
const permDashboardRead = "dashboard.dashboard.read"

type dashboardInput struct {
	MerchantID string `path:"merchantId" maxLength:"64"`
}

type dashboardOutput struct {
	Body struct {
		Merchant string `json:"merchant" doc:"The ID in the path"`
		ActingIn string `json:"acting_in" doc:"The scope the actor acts in"`
	}
}

// dashboardModule is the merchant app's own module: one route scoped with
// guard.Scope, which reads the ID from the app's own path parameter and
// asks the organisations module whether the caller is a member.
func dashboardModule() gorbital.Module {
	all := []string{orgslib.RoleOwner, orgslib.RoleAdmin, orgslib.RoleMember}
	return gorbital.Module{
		Name:        "dashboard",
		Permissions: []gorbital.Permission{{Name: permDashboardRead, Description: "See the merchant's dashboard", ScopeRoles: all}},
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			g := r.Group("/v1/merchants/{merchantId}/dashboard", gorbital.Tags("Dashboard"))
			gorbital.Get(g, "", dashboard, guard.Scope(permDashboardRead))
		},
	}
}

func dashboard(ctx context.Context, in *dashboardInput) (*dashboardOutput, error) {
	a, _ := actor.From(ctx)
	out := &dashboardOutput{}
	out.Body.Merchant, out.Body.ActingIn = in.MerchantID, a.OrgID
	return out, nil
}

// TestScopeNameMembership runs the membership scenario of
// TestOrganisationsEndToEnd against the same module mounted as merchants:
// the paths are /v1/merchants/{merchantId}/…, the refusal is
// merchant_not_found, and the app's own guard.Scope route reads the
// merchant from its own path parameter (ADR-0088).
func TestScopeNameMembership(t *testing.T) {
	a, _ := newAppWith(t, nil, nil, merchant, dashboardModule())
	h := a.Handler()
	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, bobID := signIn(t, a, "bob@example.com", "")
	carol, _ := signIn(t, a, "carol@example.com", "")

	// The personal workspace, under the app's word.
	list := do(t, h, "GET", "/v1/merchants", "", ada...)
	items, _ := list.json["items"].([]any)
	if list.code != http.StatusOK || len(items) == 0 || items[0].(map[string]any)["personal"] != true {
		t.Fatalf("GET /v1/merchants = %d %s, want the personal workspace first", list.code, list.body)
	}
	if r := do(t, h, "GET", "/v1/orgs", "", ada...); r.code != http.StatusNotFound || r.json["code"] != "not_found" {
		t.Errorf("GET /v1/orgs = %d %s, want 404 not_found: the app doesn't have organisations", r.code, r.body)
	}

	created := do(t, h, "POST", "/v1/merchants", `{"name":"Road Runners"}`, ada...)
	id, _ := created.json["id"].(string)
	if created.code != http.StatusCreated || created.json["role"] != "owner" {
		t.Fatalf("create = %d %s", created.code, created.body)
	}
	base := "/v1/merchants/" + id

	// An invitation, accepted only by the invited address.
	invited := do(t, h, "POST", base+"/invitations", `{"email":"bob@example.com","role":"admin"}`, ada...)
	if invited.code != http.StatusCreated || invited.json["role"] != "admin" {
		t.Fatalf("invite = %d %s", invited.code, invited.body)
	}
	accept := `{"token":"` + emailedInvitation(t, a, "bob@example.com") + `"}`
	if r := do(t, h, "POST", "/v1/invitations/accept", accept, carol...); r.code != http.StatusForbidden || r.json["code"] != "invitation_for_another_email" {
		t.Errorf("accept with another address = %d %s", r.code, r.body)
	}
	if r := do(t, h, "POST", "/v1/invitations/accept", accept, bob...); r.code != http.StatusOK || r.json["id"] != id {
		t.Fatalf("accept = %d %s", r.code, r.body)
	}

	// Everyone who isn't a member gets the same refusal, under the app's
	// code: the merchant, one that doesn't exist, and one that isn't an ID.
	for _, r := range []response{
		do(t, h, "GET", base, "", carol...),
		do(t, h, "GET", base+"/members", "", carol...),
		do(t, h, "GET", base+"/dashboard", "", carol...),
		do(t, h, "GET", "/v1/merchants/org_doesnotexistatallxxxxxxxx", "", carol...),
		do(t, h, "GET", "/v1/merchants/merchant_1234/dashboard", "", carol...),
	} {
		if r.code != http.StatusNotFound || r.json["code"] != "merchant_not_found" {
			t.Errorf("non-member request = %d %s, want 404 merchant_not_found", r.code, r.body)
		}
	}

	// The app's own scope route: the guard read the merchant from
	// {merchantId} and the handler acts in it.
	dash := do(t, h, "GET", base+"/dashboard", "", bob...)
	if dash.code != http.StatusOK || dash.json["merchant"] != id || dash.json["acting_in"] != id {
		t.Errorf("GET %s/dashboard = %d %s, want 200 acting in %s", base, dash.code, dash.body, id)
	}

	// Roles are the organisation roles under another name, and still
	// enforced: an admin can't delete the merchant.
	if r := do(t, h, "DELETE", base, "", bob...); r.code != http.StatusForbidden || r.json["code"] != "forbidden" {
		t.Errorf("admin deletes the merchant = %d %s, want 403 forbidden", r.code, r.body)
	}
	if r := do(t, h, "PATCH", base+"/members/"+bobID, `{"role":"owner"}`, ada...); r.code != http.StatusOK || r.json["role"] != "owner" {
		t.Errorf("make bob an owner = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", base+"/settings", "", bob...); r.code != http.StatusOK {
		t.Errorf("list the merchant's settings = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", base+"/flags", "", bob...); r.code != http.StatusOK {
		t.Errorf("list the merchant's flags = %d %s", r.code, r.body)
	}
}

// TestScopeNameDocument checks the OpenAPI document of the same app: the
// paths, the path parameter and the tag follow the app's words, and the
// operation IDs, schemas and permissions don't.
func TestScopeNameDocument(t *testing.T) {
	a, _ := newAppWith(t, nil, nil, merchant)
	spec := do(t, a.Handler(), "GET", "/openapi.json", "")
	if spec.code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d", spec.code)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			Tags        []string
			Parameters  []struct{ Name, In string }
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(spec.body), &doc); err != nil {
		t.Fatal(err)
	}
	var merchants, ids []string
	for path, item := range doc.Paths {
		if strings.HasPrefix(path, "/v1/orgs") {
			t.Errorf("%s is in the document of an app that mounted merchants", path)
		}
		if !strings.HasPrefix(path, "/v1/merchants") {
			continue
		}
		merchants = append(merchants, path)
		for method, op := range item {
			ids = append(ids, op.OperationID)
			if !slices.Contains(op.Tags, "Merchants") {
				t.Errorf("%s %s is tagged %v, want Merchants", method, path, op.Tags)
			}
			for _, p := range op.Parameters {
				if p.In == "path" && p.Name == "orgId" {
					t.Errorf("%s %s documents the path parameter orgId, want merchantId", method, path)
				}
			}
			if strings.Contains(path, "{merchantId}") && !slices.ContainsFunc(op.Parameters, func(p struct{ Name, In string }) bool {
				return p.In == "path" && p.Name == "merchantId"
			}) {
				t.Errorf("%s %s doesn't document the path parameter merchantId", method, path)
			}
		}
	}
	slices.Sort(merchants)
	want := []string{
		"/v1/merchants",
		"/v1/merchants/{merchantId}",
		"/v1/merchants/{merchantId}/flags",
		"/v1/merchants/{merchantId}/invitations",
		"/v1/merchants/{merchantId}/invitations/{invitationId}",
		"/v1/merchants/{merchantId}/invitations/{invitationId}/resend",
		"/v1/merchants/{merchantId}/leave",
		"/v1/merchants/{merchantId}/members",
		"/v1/merchants/{merchantId}/members/{userId}",
		"/v1/merchants/{merchantId}/restore",
		"/v1/merchants/{merchantId}/settings",
		"/v1/merchants/{merchantId}/settings/{key}",
		"/v1/merchants/{merchantId}/settings/{key}/history",
	}
	if !slices.Equal(merchants, want) {
		t.Errorf("paths under /v1/merchants =\n%v\nwant\n%v", merchants, want)
	}
	// Operation IDs are the declared API, and ScopeName doesn't touch them.
	for _, id := range ids {
		if !strings.HasPrefix(id, "orgs-") {
			t.Errorf("operation ID %q, want the declared orgs-* ID", id)
		}
	}
}

// TestScopeNameRefusals checks the words gorbital.New refuses, and that
// organisations' service accounts can't follow the app's words.
func TestScopeNameRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
		want string
	}{
		{"a singular that isn't a word", []Option{ScopeName("Merchant", "merchants", "merchantId"), WithoutServiceAccounts()}, "the singular"},
		{"a plural that isn't a word", []Option{ScopeName("merchant", "merchants/2", "merchantId"), WithoutServiceAccounts()}, "the plural"},
		{"a parameter that isn't one", []Option{ScopeName("merchant", "merchants", "merchant-id"), WithoutServiceAccounts()}, "the path parameter"},
		{"service accounts under another word", []Option{ScopeName("merchant", "merchants", "merchantId")}, "orgshttp.WithoutServiceAccounts()"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := authhttp.New()
			err := newModule(auth, tc.opts...).platform(&gorbital.Platform{Authenticator: auth})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("platform() error = %v, want one naming %q", err, tc.want)
			}
		})
	}
}
