package app

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/actor"
	"apistock.dev/httpx"
	orgslib "apistock.dev/modules/orgs"

	orgsmodule "example.com/acme-api/internal/modules/orgs"
	orgsdomain "example.com/acme-api/internal/modules/orgs/domain"
)

// registerOrgs wires the orgs module's operations and the errors every
// org-scoped module shares: organisations the caller can't see, and org
// roles without a permission. Error codes are public API: add new ones,
// never change existing ones.
func registerOrgs(api huma.API, mapper *httpx.Mapper, m *orgsmodule.Module) error {
	err := mapper.Add(
		httpx.Mapping{Err: actor.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		httpx.Mapping{Err: actor.ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Detail: "your role in this organisation doesn't allow this"},
		httpx.Mapping{Err: actor.ErrStepUpRequired, Status: http.StatusForbidden, Code: "mfa_required", Detail: "sign in with two-factor authentication to do this in this organisation"},
		httpx.Mapping{Err: orgslib.ErrOrgNotFound, Status: http.StatusNotFound, Code: "org_not_found", Detail: "you aren't a member of an organisation with this ID"},
		httpx.Mapping{Err: orgsdomain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		httpx.Mapping{Err: orgsdomain.ErrInvalidName, Status: http.StatusUnprocessableEntity, Code: "invalid_org_name", Detail: "an organisation name must be 1 to 100 characters on one line"},
		httpx.Mapping{Err: orgsdomain.ErrOrgVersionConflict, Status: http.StatusConflict, Code: "org_version_conflict", Detail: "the organisation changed since you read it; get it again and retry"},
		httpx.Mapping{Err: orgsdomain.ErrPersonalWorkspace, Status: http.StatusConflict, Code: "personal_workspace", Detail: "a personal workspace can't be left, deleted or shared; create an organisation instead"},
		httpx.Mapping{Err: orgsdomain.ErrMemberNotFound, Status: http.StatusNotFound, Code: "member_not_found", Detail: "the organisation has no member with this ID"},
		httpx.Mapping{Err: orgsdomain.ErrUnknownRole, Status: http.StatusUnprocessableEntity, Code: "unknown_role", Detail: "the role isn't one of the organisation roles"},
		httpx.Mapping{Err: orgsdomain.ErrRoleNotAllowed, Status: http.StatusForbidden, Code: "role_not_allowed", Detail: "you can't give, change or remove a role above your own, and only owners manage owners"},
		httpx.Mapping{Err: orgsdomain.ErrLastOwner, Status: http.StatusConflict, Code: "last_owner", Detail: "an organisation needs at least one owner; make another member an owner first"},
		httpx.Mapping{Err: orgsdomain.ErrSoleOwner, Status: http.StatusConflict, Code: "sole_owner", Detail: "you are the only owner of organisations with other members; make another member an owner, or delete them, first"},
		httpx.Mapping{Err: orgsdomain.ErrAlreadyMember, Status: http.StatusConflict, Code: "already_member", Detail: "this person is already a member"},
		httpx.Mapping{Err: orgsdomain.ErrAlreadyInvited, Status: http.StatusConflict, Code: "already_invited", Detail: "this address already has an open invitation; resend it instead"},
		httpx.Mapping{Err: orgsdomain.ErrInvitationNotFound, Status: http.StatusNotFound, Code: "invitation_not_found", Detail: "the invitation doesn't exist, was used or revoked, or expired"},
		httpx.Mapping{Err: orgsdomain.ErrInvitationEmail, Status: http.StatusForbidden, Code: "invitation_for_another_email", Detail: "the invitation was sent to another address; sign in with the invited, verified address"},
		httpx.Mapping{Err: orgsdomain.ErrTooManyInvitations, Status: http.StatusTooManyRequests, Code: "too_many_invitations", Detail: "the organisation sent too many invitations in the last hour; try again later"},
	)
	if err != nil {
		return fmt.Errorf("orgs module: %w", err)
	}
	m.Register(api)
	return nil
}
