package orgshttp

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	authhttp "example.com/acme-api/internal/modules/auth"
	"example.com/acme-api/internal/modules/orgs/usecase"
	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

// ownIdentity is the identity of an app that doesn't hand organisations
// gorbital's sign-in: it is an [Identity] and is not an
// *authhttp.Authenticator, as an app with its own accounts would write.
// It passes the organisations on to sign-in, which is what such an app
// would do from its own account hooks.
type ownIdentity struct{ auth *authhttp.Authenticator }

var _ Identity = ownIdentity{}

func (o ownIdentity) UseOrganisations(orgs authhttp.Organisations) error {
	return o.auth.UseOrganisations(orgs)
}

// TestOrganisationsWithoutTheAuthenticator mounts organisations on an
// [Identity] that isn't gorbital's sign-in and runs the membership
// scenario over HTTP: personal workspaces, an invitation accepted, and a
// non-member refused. Organisations don't require a particular sign-in
// (ADR-0088, ADR-0092).
func TestOrganisationsWithoutTheAuthenticator(t *testing.T) {
	identity := func(auth *authhttp.Authenticator) Identity { return ownIdentity{auth} }
	a, _ := newAppWith(t, nil, identity, []Option{WithoutServiceAccounts()})
	h := a.Handler()
	if _, ok := a.orgs.auth.(*authhttp.Authenticator); ok {
		t.Fatal("the module's identity is the authenticator; the test proves nothing")
	}
	ada, _ := signIn(t, a, "ada@example.com", "")
	bob, _ := signIn(t, a, "bob@example.com", "")
	carol, _ := signIn(t, a, "carol@example.com", "")

	// The identity's account hook reached organisations.
	personal := personalWorkspace(t, h, ada)
	if personal == "" {
		t.Fatal("no personal workspace")
	}
	org := newOrg(t, h, "Road Runners", ada)
	join(t, a, org, "bob@example.com", "admin", ada, bob)
	base := "/v1/orgs/" + org
	if r := do(t, h, "GET", base+"/members", "", bob...); r.code != http.StatusOK {
		t.Errorf("the new member lists members = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", base, "", carol...); r.code != http.StatusNotFound || r.json["code"] != "org_not_found" {
		t.Errorf("a non-member gets the organisation = %d %s, want 404 org_not_found", r.code, r.body)
	}

	// WithoutServiceAccounts left sign-in's operations out entirely.
	if r := do(t, h, "GET", base+"/service-accounts", "", ada...); r.code != http.StatusNotFound || r.json["code"] != "not_found" {
		t.Errorf("GET %s/service-accounts = %d %s, want 404 not_found", base, r.code, r.body)
	}
}

// TestWithoutServiceAccounts checks that the option leaves out exactly the
// operations on organisations' service accounts and the permission that
// guards them, and nothing else.
func TestWithoutServiceAccounts(t *testing.T) {
	auth := authhttp.New()
	with := operationIDs(t, Module(auth))
	without := operationIDs(t, Module(auth, WithoutServiceAccounts()))
	var dropped []string
	for _, id := range with {
		if !slices.Contains(without, id) {
			dropped = append(dropped, id)
		}
	}
	for _, id := range dropped {
		if !strings.Contains(id, "service-account") {
			t.Errorf("WithoutServiceAccounts also left out %s", id)
		}
	}
	if len(dropped) == 0 || len(without) != len(with)-len(dropped) {
		t.Errorf("operations with the option = %d, without = %d, dropped %v", len(with), len(without), dropped)
	}
	for _, id := range with {
		if strings.Contains(id, "service-account") && !slices.Contains(dropped, id) {
			t.Errorf("WithoutServiceAccounts kept %s", id)
		}
	}
	if !hasPermission(Module(auth).Permissions, usecase.PermServiceAccountsManage) {
		t.Errorf("the module doesn't declare %s", usecase.PermServiceAccountsManage)
	}
	if hasPermission(Module(auth, WithoutServiceAccounts()).Permissions, usecase.PermServiceAccountsManage) {
		t.Errorf("WithoutServiceAccounts keeps %s", usecase.PermServiceAccountsManage)
	}
}

// TestPlatformRefuses checks what gorbital.New refuses before it builds
// anything: an identity that is nobody, and one that can't register the
// service account operations the app didn't leave out.
func TestPlatformRefuses(t *testing.T) {
	auth := authhttp.New()
	for _, tc := range []struct {
		name   string
		module *module
		want   error
	}{
		{"no identity", newModule(nil), errNoAuthenticator},
		{"a nil authenticator", newModule((*authhttp.Authenticator)(nil)), errNoAuthenticator},
		{"another authenticator", newModule(auth), errNoAuthenticator},
		{"an identity that can't issue API keys", newModule(ownIdentity{auth}), errServiceAccounts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.module.platform(&gorbital.Platform{})
			if !errors.Is(err, tc.want) {
				t.Errorf("platform() error = %v, want %v", err, tc.want)
			}
		})
	}
	if err := newModule(ownIdentity{auth}, WithoutServiceAccounts()).platform(&gorbital.Platform{}); errors.Is(err, errServiceAccounts) {
		t.Errorf("WithoutServiceAccounts still asks for service accounts: %v", err)
	}
}

// operationIDs mounts m on its own API and returns its operation IDs,
// which needs no database: the operations are registered either way.
func operationIDs(t *testing.T, m gorbital.Module) []string {
	t.Helper()
	api := openapi.New(http.NewServeMux(), "acme-api", "1.0.0", openapi.WithBearerAuth("token"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := gorbital.Mount(api, mapper, gorbital.Deps{}, m); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for path, item := range api.OpenAPI().Paths {
		for _, op := range operations(item) {
			ids = append(ids, fmt.Sprintf("%s %s %s", op.Method, path, op.OperationID))
		}
	}
	slices.Sort(ids)
	return ids
}

// operations are the operations of a path, in no particular order.
func operations(item *huma.PathItem) []*huma.Operation {
	var out []*huma.Operation
	for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
		if op != nil {
			out = append(out, op)
		}
	}
	return out
}

// hasPermission reports whether the permissions declare name.
func hasPermission(perms []gorbital.Permission, name string) bool {
	return slices.ContainsFunc(perms, func(p gorbital.Permission) bool { return p.Name == name })
}
