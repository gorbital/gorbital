// Package delivery is the orgs module's HTTP adapter: Huma operations and
// request and response types for /v1/orgs and /v1/invitations.
package delivery

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/modules/openapi"
	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
	orgsusecase "example.com/invoicing/internal/modules/orgs/usecase"
	"gorbital.dev/gorbital/operation"
)

// OrgResponse is an organisation seen by one of its members.
type OrgResponse struct {
	ID        string    `json:"id" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Name      string    `json:"name"`
	Personal  bool      `json:"personal" doc:"A personal workspace: one owner, no invitations"`
	Role      string    `json:"role" example:"owner" doc:"Your role"`
	Version   int64     `json:"version" example:"1" doc:"Increases with every change; send it back when renaming"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// OrgList is the signed-in user's organisations.
type OrgList struct {
	Items []OrgResponse `json:"items"`
}

// MemberResponse is a member of an organisation.
type MemberResponse struct {
	UserID   string    `json:"user_id" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy"`
	Email    string    `json:"email"`
	Role     string    `json:"role" example:"member"`
	JoinedAt time.Time `json:"joined_at"`
}

// MemberList is an organisation's members.
type MemberList struct {
	Items []MemberResponse `json:"items"`
}

// InvitationResponse is an invitation. The token is only ever in the email.
type InvitationResponse struct {
	ID        string    `json:"id" example:"inv_mfrggzdfmztwq2lkmfrggzdfmy"`
	Email     string    `json:"email"`
	Role      string    `json:"role" example:"member"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Expired   bool      `json:"expired" doc:"An expired invitation can be resent"`
}

// InvitationList is an organisation's open invitations.
type InvitationList struct {
	Items []InvitationResponse `json:"items"`
}

type (
	orgOutput            struct{ Body OrgResponse }
	orgListOutput        struct{ Body OrgList }
	memberOutput         struct{ Body MemberResponse }
	memberListOutput     struct{ Body MemberList }
	invitationOutput     struct{ Body InvitationResponse }
	invitationListOutput struct{ Body InvitationList }
)

type orgInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
}

type createInput struct {
	Body struct {
		_    struct{} `json:"-" additionalProperties:"true"`
		Name string   `json:"name" maxLength:"100"`
	}
}

type renameInput struct {
	OrgID string `path:"orgId" maxLength:"64"`
	Body  struct {
		_       struct{} `json:"-" additionalProperties:"true"`
		Name    string   `json:"name" maxLength:"100"`
		Version int64    `json:"version" minimum:"1" doc:"The version you read. If the organisation changed since, renaming fails with org_version_conflict."`
	}
}

type memberInput struct {
	OrgID  string `path:"orgId" maxLength:"64"`
	UserID string `path:"userId" maxLength:"64"`
}

type changeRoleInput struct {
	OrgID  string `path:"orgId" maxLength:"64"`
	UserID string `path:"userId" maxLength:"64"`
	Body   struct {
		_    struct{} `json:"-" additionalProperties:"true"`
		Role string   `json:"role" maxLength:"64" example:"admin"`
	}
}

type inviteInput struct {
	OrgID string `path:"orgId" maxLength:"64"`
	Body  struct {
		_     struct{} `json:"-" additionalProperties:"true"`
		Email string   `json:"email" maxLength:"254" format:"email"`
		Role  string   `json:"role,omitempty" maxLength:"64" example:"member" default:"member"`
	}
}

type invitationInput struct {
	OrgID string `path:"orgId" maxLength:"64"`
	ID    string `path:"invitationId" maxLength:"64"`
}

type acceptInput struct {
	Body struct {
		_     struct{} `json:"-" additionalProperties:"true"`
		Token string   `json:"token" maxLength:"128" doc:"The token from the invitation link"`
	}
}

type handler struct {
	svc *orgsusecase.Service
}

// Register adds the organisation operations to r. Every operation needs a
// signed-in user; operations on an organisation answer 404 org_not_found to
// anyone who isn't a member. An API key needs each operation's permission in
// its scopes, and can't join or leave organisations (ADR-0058).
func Register(r *gorbital.Router, svc *orgsusecase.Service) {
	h := &handler{svc: svc}
	op := func(o huma.Operation) huma.Operation {
		o.Tags, o.Security = []string{"Organisations"}, openapi.Bearer
		o.Errors = append([]int{http.StatusUnauthorized}, o.Errors...)
		return o
	}
	inOrg := func(o huma.Operation) huma.Operation {
		o.Errors = append([]int{http.StatusForbidden, http.StatusNotFound}, o.Errors...)
		return op(o)
	}

	operation.Register(r, op(huma.Operation{
		OperationID: "orgs-list", Method: http.MethodGet, Path: "/v1/orgs",
		Summary: "List your organisations", Description: "Your personal workspace first, then by name.",
		Errors: []int{http.StatusForbidden},
	}), h.list)
	operation.Register(r, op(huma.Operation{
		OperationID: "orgs-create", Method: http.MethodPost, Path: "/v1/orgs",
		Summary:       "Create an organisation",
		Description:   "You become its owner. Your email address must be verified, and you can own up to `orgs.max_owned` organisations.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.create)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-get", Method: http.MethodGet, Path: "/v1/orgs/{orgId}",
		Summary: "Get an organisation",
	}), h.get)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-rename", Method: http.MethodPatch, Path: "/v1/orgs/{orgId}",
		Summary: "Rename an organisation", Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.rename)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-delete", Method: http.MethodDelete, Path: "/v1/orgs/{orgId}",
		Summary:       "Delete an organisation",
		Description:   "Members lose access at once. An owner can restore it until the retention period (`orgs.deleted_org_retention`) ends; then it is purged with its data. Personal workspaces can't be deleted.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusConflict},
	}), h.delete)
	operation.Register(r, op(huma.Operation{
		OperationID: "orgs-restore", Method: http.MethodPost, Path: "/v1/orgs/{orgId}/restore",
		Summary:     "Restore a deleted organisation",
		Description: "Needs the same role as deleting it. Restoring counts toward `orgs.max_owned` like creating.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}), h.restore)

	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-members-list", Method: http.MethodGet, Path: "/v1/orgs/{orgId}/members",
		Summary: "List members", Description: "Owners first, then in the order they joined.",
	}), h.members)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-members-change-role", Method: http.MethodPatch, Path: "/v1/orgs/{orgId}/members/{userId}",
		Summary:     "Change a member's role",
		Description: "Nobody can give or change a role above their own, only owners change owners, and the last owner can't be demoted.",
		Errors:      []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.changeRole)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-members-remove", Method: http.MethodDelete, Path: "/v1/orgs/{orgId}/members/{userId}",
		Summary: "Remove a member", DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusConflict},
	}), h.removeMember)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-leave", Method: http.MethodPost, Path: "/v1/orgs/{orgId}/leave",
		Summary: "Leave an organisation", Description: "The last owner can't leave, and nobody leaves their personal workspace. API keys can't leave organisations.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusConflict},
	}), h.leave)

	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-invitations-list", Method: http.MethodGet, Path: "/v1/orgs/{orgId}/invitations",
		Summary: "List open invitations", Description: "Invitations that weren't accepted or revoked, expired ones included, newest first.",
	}), h.invitations)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-invitations-create", Method: http.MethodPost, Path: "/v1/orgs/{orgId}/invitations",
		Summary:       "Invite someone",
		Description:   "Emails a link to the frontend page in `orgs.invitation_url`. Your email address must be verified. Accepting needs an account whose verified email is the invited address, while you are still a member who may give the role.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}), h.invite)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-invitations-resend", Method: http.MethodPost, Path: "/v1/orgs/{orgId}/invitations/{invitationId}/resend",
		Summary: "Resend an invitation", Description: "Sends a new link with a new expiry; the old link stops working. You become the invitation's inviter.",
		Errors: []int{http.StatusConflict, http.StatusTooManyRequests},
	}), h.resend)
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-invitations-revoke", Method: http.MethodDelete, Path: "/v1/orgs/{orgId}/invitations/{invitationId}",
		Summary: "Revoke an invitation", DefaultStatus: http.StatusNoContent,
	}), h.revoke)
	operation.Register(r, op(huma.Operation{
		OperationID: "invitations-accept", Method: http.MethodPost, Path: "/v1/invitations/accept",
		Summary: "Accept an invitation", Description: "API keys can't accept invitations.",
		Errors: []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict},
	}), h.accept)

	registerSettings(r, h, inOrg)
	registerFlags(r, h, inOrg)
}

func (h *handler) list(ctx context.Context, _ *struct{}) (*orgListOutput, error) {
	list, err := h.svc.List(ctx)
	if err != nil {
		return nil, err
	}
	out := &orgListOutput{Body: OrgList{Items: make([]OrgResponse, len(list))}}
	for i, m := range list {
		out.Body.Items[i] = orgResponse(m)
	}
	return out, nil
}

func (h *handler) create(ctx context.Context, in *createInput) (*orgOutput, error) {
	m, err := h.svc.Create(ctx, in.Body.Name)
	if err != nil {
		return nil, err
	}
	return &orgOutput{Body: orgResponse(m)}, nil
}

func (h *handler) get(ctx context.Context, in *orgInput) (*orgOutput, error) {
	m, err := h.svc.Get(ctx, orgslib.ID(in.OrgID))
	if err != nil {
		return nil, err
	}
	return &orgOutput{Body: orgResponse(m)}, nil
}

func (h *handler) rename(ctx context.Context, in *renameInput) (*orgOutput, error) {
	m, err := h.svc.Rename(ctx, orgslib.ID(in.OrgID), in.Body.Name, in.Body.Version)
	if err != nil {
		return nil, err
	}
	return &orgOutput{Body: orgResponse(m)}, nil
}

func (h *handler) delete(ctx context.Context, in *orgInput) (*struct{}, error) {
	return nil, h.svc.Delete(ctx, orgslib.ID(in.OrgID))
}

func (h *handler) restore(ctx context.Context, in *orgInput) (*orgOutput, error) {
	m, err := h.svc.Restore(ctx, orgslib.ID(in.OrgID))
	if err != nil {
		return nil, err
	}
	return &orgOutput{Body: orgResponse(m)}, nil
}

func (h *handler) members(ctx context.Context, in *orgInput) (*memberListOutput, error) {
	list, err := h.svc.ListMembers(ctx, orgslib.ID(in.OrgID))
	if err != nil {
		return nil, err
	}
	out := &memberListOutput{Body: MemberList{Items: make([]MemberResponse, len(list))}}
	for i, m := range list {
		out.Body.Items[i] = memberResponse(m)
	}
	return out, nil
}

func (h *handler) changeRole(ctx context.Context, in *changeRoleInput) (*memberOutput, error) {
	m, err := h.svc.ChangeRole(ctx, orgslib.ID(in.OrgID), in.UserID, in.Body.Role)
	if err != nil {
		return nil, err
	}
	return &memberOutput{Body: memberResponse(m)}, nil
}

func (h *handler) removeMember(ctx context.Context, in *memberInput) (*struct{}, error) {
	return nil, h.svc.RemoveMember(ctx, orgslib.ID(in.OrgID), in.UserID)
}

func (h *handler) leave(ctx context.Context, in *orgInput) (*struct{}, error) {
	return nil, h.svc.Leave(ctx, orgslib.ID(in.OrgID))
}

func (h *handler) invitations(ctx context.Context, in *orgInput) (*invitationListOutput, error) {
	list, err := h.svc.ListInvitations(ctx, orgslib.ID(in.OrgID))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := &invitationListOutput{Body: InvitationList{Items: make([]InvitationResponse, len(list))}}
	for i, inv := range list {
		out.Body.Items[i] = invitationResponse(inv, now)
	}
	return out, nil
}

func (h *handler) invite(ctx context.Context, in *inviteInput) (*invitationOutput, error) {
	inv, err := h.svc.Invite(ctx, orgslib.ID(in.OrgID), in.Body.Email, in.Body.Role)
	if err != nil {
		return nil, err
	}
	return &invitationOutput{Body: invitationResponse(inv, time.Now())}, nil
}

func (h *handler) resend(ctx context.Context, in *invitationInput) (*invitationOutput, error) {
	inv, err := h.svc.ResendInvitation(ctx, orgslib.ID(in.OrgID), in.ID)
	if err != nil {
		return nil, err
	}
	return &invitationOutput{Body: invitationResponse(inv, time.Now())}, nil
}

func (h *handler) revoke(ctx context.Context, in *invitationInput) (*struct{}, error) {
	return nil, h.svc.RevokeInvitation(ctx, orgslib.ID(in.OrgID), in.ID)
}

func (h *handler) accept(ctx context.Context, in *acceptInput) (*orgOutput, error) {
	m, err := h.svc.AcceptInvitation(ctx, in.Body.Token)
	if err != nil {
		return nil, err
	}
	return &orgOutput{Body: orgResponse(m)}, nil
}

func orgResponse(m orgsdomain.Membership) OrgResponse {
	return OrgResponse{
		ID: m.ID, Name: m.Name, Personal: m.Personal, Role: m.Role,
		Version: m.Version, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func memberResponse(m orgsdomain.Member) MemberResponse {
	return MemberResponse{UserID: m.UserID, Email: m.Email, Role: m.Role, JoinedAt: m.JoinedAt}
}

func invitationResponse(inv orgsdomain.Invitation, now time.Time) InvitationResponse {
	return InvitationResponse{
		ID: inv.ID, Email: inv.Email, Role: inv.Role, CreatedAt: inv.CreatedAt, ExpiresAt: inv.ExpiresAt,
		Expired: !now.Before(inv.ExpiresAt),
	}
}
