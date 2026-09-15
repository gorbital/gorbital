package orgs

import (
	"context"
	"errors"

	"gorbital.dev/actor"
	"gorbital.dev/modules/auth"
)

// Member is a user's membership in an organisation.
type Member struct {
	OrgID  ID
	UserID string
	Role   string
}

// Memberships reads memberships. The app's orgs repository implements it.
type Memberships interface {
	// MemberRole returns userID's role in orgID, or [ErrNotMember] when the
	// user isn't a member or the organisation is deleted.
	MemberRole(ctx context.Context, orgID ID, userID string) (string, error)
}

// RequireMember checks that the signed-in user is a member of orgID whose
// role grants permission, and returns a context whose actor acts in that
// organisation: OrgID is set and Permissions are the role's permissions in
// catalog, replacing platform permissions for the operation. Every
// organisation operation calls it first (ADR-0048).
//
// It returns [actor.ErrUnauthenticated] without a signed-in user,
// [ErrOrgNotFound] when orgID isn't one of the user's live organisations,
// [actor.ErrStepUpRequired] when the role grants permission only to
// sessions verified with a second factor, and [actor.ErrForbidden] when it
// doesn't grant it.
//
// The membership is read on every call, so removing a member takes effect
// on their next request.
func RequireMember(ctx context.Context, m Memberships, catalog *auth.Catalog, orgID ID, permission string) (context.Context, Member, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind != actor.KindUser || a.ID == "" {
		return ctx, Member{}, actor.ErrUnauthenticated
	}
	if _, err := ParseID(string(orgID)); err != nil {
		return ctx, Member{}, ErrOrgNotFound
	}
	role, err := m.MemberRole(ctx, orgID, a.ID)
	if err != nil {
		if errors.Is(err, ErrNotMember) {
			return ctx, Member{}, ErrOrgNotFound
		}
		return ctx, Member{}, err
	}

	p, _ := auth.PrincipalFrom(ctx)
	a.OrgID = string(orgID)
	a.Permissions, a.StepUp = catalog.PermissionsFor([]string{role}, p.MFAVerified)
	ctx = actor.With(ctx, a)
	member := Member{OrgID: orgID, UserID: a.ID, Role: role}
	if err := actor.Require(ctx, permission); err != nil {
		return ctx, member, err
	}
	return ctx, member, nil
}
