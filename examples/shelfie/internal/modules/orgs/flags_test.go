package orgshttp

import (
	"fmt"
	"net/http"
	"testing"

	"gorbital.dev/modules/flags"
)

// TestOrganisationFlagsEndToEnd checks that members read feature flags as
// the organisation they act in (ADR-0057): its allow and deny lists apply
// before user lists, it is the subject of percentage rollouts so members
// share its answer, and outside it each user gets their own.
func TestOrganisationFlagsEndToEnd(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	admin, _ := signIn(t, a, "admin@example.com", "platform_admin")
	ada, adaID := signIn(t, a, "ada@example.com", "")
	bob, bobID := signIn(t, a, "bob@example.com", "")

	created := do(t, h, "POST", "/v1/orgs", `{"name":"Road Runners"}`, ada...)
	org, _ := created.json["id"].(string)
	if created.code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.code, created.body)
	}
	do(t, h, "POST", "/v1/orgs/"+org+"/invitations", `{"email":"bob@example.com"}`, ada...)
	if r := do(t, h, "POST", "/v1/invitations/accept", fmt.Sprintf(`{"token":%q}`, emailedInvitation(t, a, "bob@example.com")), bob...); r.code != http.StatusOK {
		t.Fatalf("bob accepts = %d %s", r.code, r.body)
	}
	orgFlags := "/v1/orgs/" + org + "/flags"
	if r := do(t, h, "GET", orgFlags, ""); r.code != http.StatusUnauthorized {
		t.Errorf("GET %s without a session = %d, want 401", orgFlags, r.code)
	}

	// The organisation is allowed and Bob is denied: in the organisation the
	// organisation's rule decides, outside it Bob's.
	body := flagState(0, "organisation beta", fmt.Sprintf(`{"enabled":true,"default":false,"orgs":{"allow":[%q],"deny":[]},"users":{"allow":[],"deny":[%q]}}`, org, bobID))
	if r := do(t, h, "PUT", pingTimeFlag, body, admin...); r.code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s", pingTimeFlag, r.code, r.body)
	}
	for _, tt := range []struct {
		who     string
		path    string
		headers []string
		want    bool
	}{
		{"ada in the organisation", orgFlags, ada, true},
		{"bob in the organisation", orgFlags, bob, true},
		{"ada outside it", "/v1/flags", ada, false},
		{"bob outside it", "/v1/flags", bob, false},
	} {
		if got := clientFlags(t, h, tt.path, tt.headers...)["example.ping_time"]; got != tt.want {
			t.Errorf("%s: example.ping_time = %v, want %v", tt.who, got, tt.want)
		}
	}

	// A 50% rollout: members share the organisation's bucket; outside it,
	// each user has their own.
	body = flagState(1, "half", `{"enabled":true,"default":false,"orgs":{"allow":[],"deny":[]},"users":{"allow":[],"deny":[]},"percentage":50}`)
	if r := do(t, h, "PUT", pingTimeFlag, body, admin...); r.code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s", pingTimeFlag, r.code, r.body)
	}
	inOrg := flags.Bucket("example.ping_time", org) < 50
	for _, member := range []struct {
		id      string
		headers []string
	}{{adaID, ada}, {bobID, bob}} {
		if got := clientFlags(t, h, orgFlags, member.headers...)["example.ping_time"]; got != inOrg {
			t.Errorf("%s in the organisation: example.ping_time = %v, want the organisation's %v", member.id, got, inOrg)
		}
		if got, want := clientFlags(t, h, "/v1/flags", member.headers...)["example.ping_time"], flags.Bucket("example.ping_time", member.id) < 50; got != want {
			t.Errorf("%s outside the organisation: example.ping_time = %v, want their own %v", member.id, got, want)
		}
	}
}

// TestOrganisationsCantReachEachOthersFlags is the cross-organisation denial
// test for organisation flags: a member of one organisation can't read
// another's, gets the same 404 as for an organisation that doesn't exist,
// and the other's allow list doesn't reach their own organisation.
func TestOrganisationsCantReachEachOthersFlags(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	admin, _ := signIn(t, a, "admin@example.com", "platform_admin")
	ada, _ := signIn(t, a, "ada@example.com", "")
	carol, _ := signIn(t, a, "carol@example.com", "")
	adaOrg, carolOrg := personalWorkspace(t, h, ada), personalWorkspace(t, h, carol)

	body := flagState(0, "ada's workspace", fmt.Sprintf(`{"enabled":true,"default":false,"orgs":{"allow":[%q],"deny":[]},"users":{"allow":[],"deny":[]}}`, adaOrg))
	if r := do(t, h, "PUT", pingTimeFlag, body, admin...); r.code != http.StatusOK {
		t.Fatalf("PUT %s = %d %s", pingTimeFlag, r.code, r.body)
	}
	if got := clientFlags(t, h, "/v1/orgs/"+adaOrg+"/flags", ada...)["example.ping_time"]; got != true {
		t.Errorf("ada in her workspace: example.ping_time = %v, want true", got)
	}
	for _, path := range []string{"/v1/orgs/" + adaOrg + "/flags", "/v1/orgs/org_doesnotexist/flags"} {
		if r := do(t, h, "GET", path, "", carol...); r.code != http.StatusNotFound || r.json["code"] != "org_not_found" {
			t.Errorf("carol: GET %s = %d %s, want 404 org_not_found", path, r.code, r.body)
		}
	}
	if got := clientFlags(t, h, "/v1/orgs/"+carolOrg+"/flags", carol...)["example.ping_time"]; got != false {
		t.Errorf("carol in her own workspace: example.ping_time = %v, want false", got)
	}
}
