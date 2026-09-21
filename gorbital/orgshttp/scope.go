package orgshttp

import (
	"gorbital.dev/gorbital"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"
)

// DefaultScope is the tenancy organisations give an app (ADR-0088): the
// concept named "organisation", its ID in {orgId} and shaped like
// orgs.ParseID accepts, refused with 404 org_not_found, the roles owner,
// admin and member, and the organisation carried into the request's
// connections for row-level security. It is the vocabulary of v0.1 and
// v0.2 multi-tenant apps, so their HTTP contract doesn't change.
//
// [Module] sets it with gorbital.Platform.SetScope, so an app that mounts
// organisations needs nothing else for guard.Scope. An app that writes its
// own membership rules starts from its own gorbital.Scope instead.
//
// Unlike gorbital.DefaultOrgScope it has a ValidID: only this package may
// import gorbital.dev/modules/orgs, so only this package knows how an
// organisation ID is shaped.
func DefaultScope() gorbital.Scope {
	return gorbital.Scope{
		Name:         "organisation",
		PathParam:    "orgId",
		NotFoundCode: "org_not_found",
		ValidID:      validOrgID,
		Roles: []gorbital.ScopeRole{
			{Name: orgslib.RoleOwner, Description: "Everything, including deleting the organisation and managing owners"},
			{Name: orgslib.RoleAdmin, Description: "Manages the organisation and its members, except owners"},
			{Name: orgslib.RoleMember, Description: "Works in the organisation"},
		},
		Session: postgres.WithScope,
	}
}

// validOrgID reports whether id is shaped like an organisation ID. A
// malformed ID is refused exactly as an unknown one is, before the
// authorizer is asked, so it never reaches a query.
func validOrgID(id string) bool {
	_, err := orgslib.ParseID(id)
	return err == nil
}
