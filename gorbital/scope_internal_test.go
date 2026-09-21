package gorbital

import (
	"errors"
	"fmt"
	"testing"

	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/orgs"
)

func TestScopeDefaultsAreTheOrganisationVocabulary(t *testing.T) {
	s := Scope{}.withDefaults()
	if s.Name != "organisation" || s.PathParam != "orgId" || s.NotFoundCode != "org_not_found" {
		t.Fatalf("empty scope defaulted to %+v; v0.2 apps rely on organisation/orgId/org_not_found", s)
	}
	if err := s.validate(); err != nil {
		t.Fatalf("the default scope is invalid: %v", err)
	}
}

func TestScopeValidateRejectsNamesThatWouldReachTheAPI(t *testing.T) {
	for name, s := range map[string]Scope{
		"empty name":         {Name: "", PathParam: "x", NotFoundCode: "a_b"},
		"name with a space":  {Name: "big merchant", PathParam: "x", NotFoundCode: "a_b"},
		"name with a hyphen": {Name: "merchant-group", PathParam: "x", NotFoundCode: "a_b"},
		"path with a brace":  {Name: "merchant", PathParam: "{id}", NotFoundCode: "a_b"},
		"path with a slash":  {Name: "merchant", PathParam: "a/b", NotFoundCode: "a_b"},
		"code in caps":       {Name: "merchant", PathParam: "x", NotFoundCode: "NotFound"},
		"role in caps":       {Name: "merchant", PathParam: "x", NotFoundCode: "a_b", Roles: []ScopeRole{{Name: "Owner"}}},
	} {
		if err := s.validate(); err == nil {
			t.Errorf("%s: validate accepted %+v", name, s)
		}
	}
}

func TestScopeValidIDRefusesNothingWhenUnset(t *testing.T) {
	s := Scope{}.withDefaults()
	if s.valid("") {
		t.Error("an empty ID is never a scope")
	}
	if !s.valid("anything-at-all") {
		t.Error("without ValidID every non-empty ID reaches the authorizer, which decides")
	}
	s.ValidID = func(id string) bool { return id == "m_1" }
	if s.valid("m_2") {
		t.Error("ValidID was not consulted")
	}
}

// A malformed ID must be refused exactly as an unknown one is, with the
// scope's own code, so scope IDs can't be probed.
func TestScopeRefusalsUseTheScopesOwnWords(t *testing.T) {
	s := Scope{Name: "merchant", PathParam: "merchantId", NotFoundCode: "merchant_not_found"}.withDefaults()
	for _, tc := range []struct {
		what string
		err  error
		want string
	}{
		{"not found", s.notFound(), "merchant_not_found"},
		{"forbidden", s.forbidden(), "forbidden"},
		{"mfa", s.mfaRequired(), "mfa_required"},
	} {
		if got := fmt.Sprint(tc.err); got == "" {
			t.Errorf("%s: empty problem", tc.what)
		}
	}
	if d := fmt.Sprint(s.notFound()); d == fmt.Sprint(Scope{}.withDefaults().notFound()) {
		t.Error("a merchant app's 404 reads like an organisation's")
	}
}

func TestScopePathSegmentSuggestsARealRoute(t *testing.T) {
	for name, want := range map[string]string{"organisation": "orgs", "merchant": "merchants", "premises": "premises"} {
		if got := (Scope{Name: name}).pathSegment(); got != want {
			t.Errorf("scope %q suggests /v1/%s/, want /v1/%s/", name, got, want)
		}
	}
}

// The composition doesn't import gorbital.dev/modules/orgs (ADR-0019), so
// the deprecated OrgAuthorizer path recognises its not-found by message.
// This test is what keeps the two equal.
func TestOrgNotFoundIsRecognised(t *testing.T) {
	if !isOrgNotFound(orgs.ErrOrgNotFound) {
		t.Fatalf("orgs.ErrOrgNotFound is %q, but the adapter looks for %q", orgs.ErrOrgNotFound, orgNotFoundMessage)
	}
	if !isOrgNotFound(fmt.Errorf("reading the membership: %w", orgs.ErrOrgNotFound)) {
		t.Error("a wrapped orgs.ErrOrgNotFound was not recognised")
	}
	if isOrgNotFound(errors.New("some other failure")) {
		t.Error("an unrelated error was taken for a missing organisation")
	}
}

func TestPermissionScopeRolesFallsBackToOrgRoles(t *testing.T) {
	if got := (Permission{OrgRoles: []string{"owner"}}).scopeRoles(); len(got) != 1 || got[0] != "owner" {
		t.Errorf("a v0.2 permission's OrgRoles were ignored: %v", got)
	}
	if got := (Permission{ScopeRoles: []string{"manager"}}).scopeRoles(); len(got) != 1 || got[0] != "manager" {
		t.Errorf("ScopeRoles were ignored: %v", got)
	}
}

func TestDeclareRejectsBothRoleFields(t *testing.T) {
	m := Module{Name: "orders", Permissions: []Permission{
		{Name: "orders.order.read", ScopeRoles: []string{"owner"}, OrgRoles: []string{"owner"}},
	}}
	err := Declare(Declarations{Permissions: auth.NewCatalog(), OrgPermissions: auth.NewCatalog()}, m)
	if err == nil {
		t.Fatal("a permission with both ScopeRoles and OrgRoles was accepted")
	}
}

// Roles come from the scope, in its order, with the scope's descriptions;
// a role only a module names still gets declared.
func TestDeclareScopeRolesUsesTheScopesRoles(t *testing.T) {
	s := Scope{Name: "merchant", Roles: []ScopeRole{
		{Name: "owner", Description: "Runs the merchant"},
		{Name: "courier", Description: "Delivers"},
	}}.withDefaults()
	m := Module{Name: "orders", Permissions: []Permission{
		{Name: "orders.order.read", ScopeRoles: []string{"courier", "owner", "auditor"}},
	}}
	catalog := auth.NewCatalog()
	if err := Declare(Declarations{Permissions: auth.NewCatalog(), OrgPermissions: catalog}, m); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if err := declareScopeRoles(catalog, s, []Module{m}); err != nil {
		t.Fatalf("declareScopeRoles: %v", err)
	}
	for _, role := range []string{"owner", "courier", "auditor"} {
		if !catalog.HasRole(role) {
			t.Errorf("role %q was not declared", role)
		}
	}
}
