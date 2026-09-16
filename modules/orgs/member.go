package orgs

import (
	"context"
	"errors"

	"gorbital.dev/actor"
	"gorbital.dev/modules/auth"
)

// Member is a user's membership in an organisation, or an organisation
// service account's role in its organisation (ADR-0058): then UserID is the
// service account's ID.
type Member struct {
	OrgID  ID
	UserID string
	Role   string
}

// Memberships reads memberships. The app's orgs repository implements it.
type Memberships interface {
	// MemberRole returns userID's role in orgID, or [ErrNotMember] when the
	// user isn't a member or the organisation is deleted. An implementation
	// that also answers for service account IDs returns the role of an
	// enabled service account of that organisation.
	MemberRole(ctx context.Context, orgID ID, userID string) (string, error)
}

// RequireMember checks that the signed-in user is a member of orgID whose
// role grants permission, and returns a context whose actor acts in that
// organisation: OrgID is set and Permissions are the role's permissions in
// catalog, replacing platform permissions for the operation. Every
// organisation operation calls it first (ADR-0048).
//
// A service account's API key acts like a member when m answers for its ID
// (ADR-0058); see [Authorize] for what it may reach.
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
	a, ok := memberActor(ctx)
	if !ok {
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
	member := Member{OrgID: orgID, UserID: a.ID, Role: role}
	ctx, err = Authorize(ctx, catalog, member, permission)
	return ctx, member, err
}

// Authorize is [RequireMember] for a membership the caller has already
// read, such as one in a deleted organisation, which [Memberships] doesn't
// return: it checks that member is the signed-in user and that their role
// grants permission, with the same two-factor step-up, and returns a
// context whose actor acts in member's organisation. Every permission check
// on an organisation goes through it, so roles that require two-factor
// authentication can't be bypassed by a path that reads the role itself.
//
// For a request authenticated with an API key, the role's permissions are
// limited by [auth.Principal.Restrict]: never those of roles that require
// two-factor authentication, and only the key's scopes. A service account's
// key reaches only the organisation the service account belongs to.
//
// It returns [actor.ErrUnauthenticated] without a signed-in user,
// [ErrOrgNotFound] when member belongs to another user or has an invalid
// organisation ID, or a service account's key is used for another
// organisation, [actor.ErrStepUpRequired] when the role grants permission
// only to sessions verified with a second factor, and [actor.ErrForbidden]
// when it doesn't grant it. The returned context is usable, with the role's
// permissions, even when the error is ErrStepUpRequired or ErrForbidden.
func Authorize(ctx context.Context, catalog *auth.Catalog, member Member, permission string) (context.Context, error) {
	a, ok := memberActor(ctx)
	if !ok {
		return ctx, actor.ErrUnauthenticated
	}
	if _, err := ParseID(string(member.OrgID)); err != nil || member.UserID != a.ID {
		return ctx, ErrOrgNotFound
	}
	p, _ := auth.PrincipalFrom(ctx)
	if a.Kind == actor.KindService && p.OrgID != string(member.OrgID) {
		return ctx, ErrOrgNotFound
	}
	a.OrgID = string(member.OrgID)
	a.Permissions, a.StepUp = p.Restrict(catalog.PermissionsFor([]string{member.Role}, p.MFAVerified))
	ctx = actor.With(ctx, a)
	if err := actor.Require(ctx, permission); err != nil {
		return ctx, err
	}
	return ctx, nil
}

// memberActor returns the actor that may act in an organisation: a signed-in
// user, or a service account authenticated by its API key, whose principal
// names it (ADR-0058); Authorize keeps a service account to its own
// organisation. Service actors set another way, such as by jobs, are not
// members of anything.
func memberActor(ctx context.Context) (actor.Actor, bool) {
	a, ok := actor.From(ctx)
	switch {
	case !ok || a.ID == "":
		return a, false
	case a.Kind == actor.KindUser:
		return a, true
	case a.Kind == actor.KindService:
		p, ok := auth.PrincipalFrom(ctx)
		return a, ok && p.APIKey() && p.ServiceAccountID == a.ID
	default:
		return a, false
	}
}
