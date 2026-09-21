package gorbital

import (
	"context"
	"errors"
	"fmt"

	"gorbital.dev/modules/postgres"
)

// An OrgAuthorizer decides whether the actor of a request may act in an
// organisation.
//
// Deprecated: organisations are one [Scope] (ADR-0088). Use
// [ScopeAuthorizer], whose AuthorizeScope has exactly these semantics.
// This interface, [Platform.SetOrgAuthorizer] and guard.OrgMember keep
// working for all of v0.x.
//
// AuthorizeOrg checks that the actor in ctx is a member of orgID (a user,
// or a service account of that organisation authenticated by its API key)
// whose role grants permission, and returns a context whose actor acts in
// the organisation: actor.Actor.OrgID set, and Permissions those of the
// role, limited by an API key's scopes. Its errors are
// [ScopeAuthorizer]'s, with orgs.ErrOrgNotFound accepted in place of
// [ErrScopeNotFound].
type OrgAuthorizer interface {
	AuthorizeOrg(ctx context.Context, orgID, permission string) (context.Context, error)
}

// SetOrgAuthorizer makes a the app's organisation authorizer.
//
// Deprecated: use [Platform.SetScope] with a [Scope], which lets the app
// name its own tenancy and validate its own IDs. This call is
// [Platform.SetScope] with the default organisation scope: the v0.1 and
// v0.2 vocabulary, and no ID validation of its own, so a malformed ID is
// refused by the authorizer exactly as an unknown one is.
func (p *Platform) SetOrgAuthorizer(a OrgAuthorizer) error {
	if a == nil {
		return errors.New("the organisation authorizer is nil")
	}
	return p.app.setScope(DefaultOrgScope(), orgAdapter{a}, "set by an organisations module")
}

// DefaultOrgScope is the tenancy of v0.1 and v0.2 organisation apps: the
// concept named "organisation" in {orgId}, refused with org_not_found, the
// roles owner, admin and member, and the organisation carried into the
// request's connections for row-level security.
//
// It has no ValidID: gorbital does not know how an app's organisation IDs
// are shaped. gorbital.dev/gorbital/orgshttp supplies orgs.ParseID with
// its own DefaultScope.
func DefaultOrgScope() Scope {
	return Scope{
		Name:         DefaultScopeName,
		PathParam:    DefaultScopePathParam,
		NotFoundCode: DefaultScopeNotFoundCode,
		Roles: []ScopeRole{
			{Name: "owner", Description: "Everything, including deleting the organisation and managing owners"},
			{Name: "admin", Description: "Manages the organisation and its members, except owners"},
			{Name: "member", Description: "Works in the organisation"},
		},
		Session: postgres.WithScope,
	}
}

// orgAdapter presents an [OrgAuthorizer] as a [ScopeAuthorizer].
type orgAdapter struct{ a OrgAuthorizer }

func (o orgAdapter) AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error) {
	scoped, err := o.a.AuthorizeOrg(ctx, scopeID, permission)
	if err != nil && isOrgNotFound(err) {
		// orgs.ErrOrgNotFound means what ErrScopeNotFound means. The
		// composition doesn't import gorbital.dev/modules/orgs
		// (ADR-0019), so this deprecated path recognises it by message
		// and the guard sees one sentinel. TestOrgNotFoundIsRecognised
		// proves the message stays equal to it.
		return scoped, fmt.Errorf("%w: %w", ErrScopeNotFound, err)
	}
	return scoped, err
}

// orgNotFoundMessage is orgs.ErrOrgNotFound's message.
const orgNotFoundMessage = "orgs: organisation not found"

// isOrgNotFound reports whether err is, or wraps, orgs.ErrOrgNotFound.
func isOrgNotFound(err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if err.Error() == orgNotFoundMessage {
			return true
		}
	}
	return false
}
