package domain

import "errors"

// Errors returned by the orgs use cases. Their HTTP codes are mapped in
// orgshttp's module.go. An organisation the caller can't see is
// orgs.ErrOrgNotFound from gorbital.dev/modules/orgs.
var (
	// ErrUnauthenticated reports an operation without a signed-in user.
	ErrUnauthenticated = errors.New("authentication is required")
	// ErrForbidden reports a signed-in user without a platform permission
	// the orgs module checks: an API key whose scopes don't include it.
	ErrForbidden = errors.New("missing permission")
	// ErrSessionRequired reports an API key used to join or leave an
	// organisation, which needs the person's session.
	ErrSessionRequired = errors.New("a session is required")
	// ErrInvalidName reports a blank, too long or multi-line name.
	ErrInvalidName = errors.New("invalid organisation name")
	// ErrOrgVersionConflict reports a change to a version that is no longer
	// current.
	ErrOrgVersionConflict = errors.New("organisation was changed since it was read")
	// ErrOrgNameTaken reports renaming a restaurant to a name another
	// restaurant on the platform uses. A restaurant is an organisation, and
	// the restaurants module's index (orgs_restaurant_name) keeps their names
	// unique.
	ErrOrgNameTaken = errors.New("another restaurant uses this name")
	// ErrPersonalWorkspace reports something a personal workspace doesn't
	// allow: leaving, deleting, or inviting people.
	ErrPersonalWorkspace = errors.New("not allowed in a personal workspace")
	// ErrMemberNotFound reports a user who isn't a member.
	ErrMemberNotFound = errors.New("member not found")
	// ErrUnknownRole reports a role the org catalog doesn't declare.
	ErrUnknownRole = errors.New("unknown organisation role")
	// ErrRoleNotAllowed reports a role above the caller's own, or owners
	// changed by someone who isn't one.
	ErrRoleNotAllowed = errors.New("role not allowed")
	// ErrLastOwner reports a change that would leave an organisation
	// without an owner.
	ErrLastOwner = errors.New("an organisation needs at least one owner")
	// ErrSoleOwner reports an account that can't be deleted while it is the
	// only owner of an organisation with other members. The error is a
	// *SoleOwnerError.
	ErrSoleOwner = errors.New("sole owner of an organisation with other members")
	// ErrAlreadyMember reports inviting or adding someone who is a member.
	ErrAlreadyMember = errors.New("already a member")
	// ErrAlreadyInvited reports inviting an address that has an open
	// invitation; resend it instead.
	ErrAlreadyInvited = errors.New("already invited")
	// ErrInvitationNotFound reports an invitation that doesn't exist, was
	// used or revoked, or expired.
	ErrInvitationNotFound = errors.New("invitation not found")
	// ErrInvitationEmail reports accepting an invitation sent to another
	// address, or with an address that isn't verified.
	ErrInvitationEmail = errors.New("invitation is for another email address")
	// ErrTooManyInvitations reports more invitations than an organisation,
	// or a user across organisations, may send per hour.
	ErrTooManyInvitations = errors.New("too many invitations")
	// ErrEmailNotVerified reports creating an organisation or sending an
	// invitation from an account whose email address isn't verified.
	ErrEmailNotVerified = errors.New("email address not verified")
	// ErrTooManyOrgs reports creating or restoring an organisation when the
	// user already owns as many as allowed.
	ErrTooManyOrgs = errors.New("too many organisations")
)

// SoleOwnerError lists the organisations that stop an account's deletion.
type SoleOwnerError struct {
	Orgs []SoleOwnership
}

func (e *SoleOwnerError) Error() string { return ErrSoleOwner.Error() }

// Unwrap returns ErrSoleOwner.
func (e *SoleOwnerError) Unwrap() error { return ErrSoleOwner }
