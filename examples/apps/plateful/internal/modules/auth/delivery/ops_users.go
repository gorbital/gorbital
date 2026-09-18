package delivery

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/modules/openapi"

	authdomain "example.com/plateful/internal/modules/auth/domain"
	authusecase "example.com/plateful/internal/modules/auth/usecase"
)

// Operators' account APIs under /ops/auth/users (ADR-0070), for the Dev
// Portal's Authentication screen and for scripts. They need
// ops.auth.read for reads and ops.auth.write for writes: the platform
// administrator's role holds both, and so does the development operator.

// OpsUserResponse is an account as operators see it.
type OpsUserResponse struct {
	UserResponse
	BannedAt     *time.Time `json:"banned_at,omitempty"`
	BannedReason string     `json:"banned_reason,omitempty"`
}

// OpsUserList is a page of accounts, newest first.
type OpsUserList struct {
	Users      []OpsUserResponse `json:"users"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Pass as cursor for the next page; absent on the last page"`
}

// OpsCodeResponse is a usable verification or reset code, without the
// code: codes are stored hashed and arrive by email (the Dev Portal reads
// them from the local inbox).
type OpsCodeResponse struct {
	ID          string    `json:"id"`
	Purpose     string    `json:"purpose" enum:"verify_email,reset_password"`
	Attempts    int       `json:"attempts"`
	MaxAttempts int       `json:"max_attempts"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// OpsMFAStatus describes an account's second factors.
type OpsMFAStatus struct {
	TOTP          bool `json:"totp" doc:"A confirmed authenticator app"`
	RecoveryCodes int  `json:"recovery_codes" doc:"Unused recovery codes"`
	Passkeys      int  `json:"passkeys"`
}

// OpsUserDetail is everything about one account.
type OpsUserDetail struct {
	User       OpsUserResponse    `json:"user"`
	Sessions   []SessionResponse  `json:"sessions"`
	Passkeys   []PasskeyResponse  `json:"passkeys"`
	Identities []IdentityResponse `json:"identities"`
	MFA        OpsMFAStatus       `json:"mfa"`
	Codes      []OpsCodeResponse  `json:"codes"`
}

// OpsImpersonationResponse is a session started for a user by an operator.
type OpsImpersonationResponse struct {
	Token       string          `json:"token" doc:"Send as Authorization: Bearer; shown once"`
	Session     SessionResponse `json:"session"`
	User        OpsUserResponse `json:"user"`
	MFAVerified bool            `json:"mfa_verified"`
}

// OpsRevokedResponse counts ended sessions.
type OpsRevokedResponse struct {
	Revoked int64 `json:"revoked"`
}

// OpsTOTPEnrollmentResponse is an authenticator app secret and recovery
// codes an operator turned on for a user, shown once.
type OpsTOTPEnrollmentResponse struct {
	Secret        string   `json:"secret"`
	URI           string   `json:"uri" doc:"otpauth:// URI for authenticator apps"`
	RecoveryCodes []string `json:"recovery_codes"`
}

type opsUserListInput struct {
	Query  string `query:"q" maxLength:"200" doc:"Part of the email address, or an ID"`
	Cursor string `query:"cursor" maxLength:"200"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"50"`
}
type opsUserListOutput struct{ Body OpsUserList }
type opsUserInput struct {
	ID string `path:"id" maxLength:"64"`
}
type opsUserDetailOutput struct{ Body OpsUserDetail }
type opsUserOutput struct{ Body OpsUserResponse }
type opsCreateUserInput struct {
	Body struct {
		_             struct{} `json:"-" additionalProperties:"true"`
		Email         string   `json:"email" maxLength:"254" example:"ada@example.com"`
		Password      string   `json:"password" maxLength:"512" doc:"At least 12 characters"`
		EmailVerified bool     `json:"email_verified,omitempty" doc:"Skip verification, as for seed data"`
	}
}
type opsBanInput struct {
	ID   string `path:"id" maxLength:"64"`
	Body struct {
		_      struct{} `json:"-" additionalProperties:"true"`
		Reason string   `json:"reason,omitempty" maxLength:"500"`
	}
}
type opsRoleInput struct {
	ID   string `path:"id" maxLength:"64"`
	Body struct {
		_    struct{} `json:"-" additionalProperties:"true"`
		Role string   `json:"role" maxLength:"64" example:"platform_admin"`
	}
}
type opsRolePathInput struct {
	ID   string `path:"id" maxLength:"64"`
	Role string `path:"role" maxLength:"64"`
}
type opsSessionInput struct {
	ID        string `path:"id" maxLength:"64"`
	SessionID string `path:"sessionId" maxLength:"64"`
}
type opsPasskeyInput struct {
	ID        string `path:"id" maxLength:"64"`
	PasskeyID string `path:"passkeyId" maxLength:"64"`
}
type opsIdentityInput struct {
	ID         string `path:"id" maxLength:"64"`
	IdentityID string `path:"identityId" maxLength:"64"`
}
type opsImpersonateInput struct {
	ID   string `path:"id" maxLength:"64"`
	Body struct {
		_           struct{} `json:"-" additionalProperties:"true"`
		MFAVerified bool     `json:"mfa_verified,omitempty" doc:"Count the session as signed in with a second factor, so roles that require one apply"`
	}
}
type opsImpersonateOutput struct{ Body OpsImpersonationResponse }
type opsRevokedOutput struct{ Body OpsRevokedResponse }
type opsTOTPEnrollmentOutput struct{ Body OpsTOTPEnrollmentResponse }

// registerOpsUsers adds the operators' account APIs.
func registerOpsUsers(r *routes, h *handler) {
	ops := func(op huma.Operation) huma.Operation {
		op.Tags, op.Security = []string{"Ops: auth"}, openapi.Bearer
		op.Errors = append([]int{http.StatusUnauthorized, http.StatusForbidden}, op.Errors...)
		return op
	}
	notFound := []int{http.StatusNotFound}
	route(r, ops(huma.Operation{
		OperationID: "ops-list-users", Method: http.MethodGet, Path: "/ops/auth/users",
		Summary: "List accounts", Description: "Newest first; `q` matches part of the address or an ID. Deleted accounts aren't listed; banned ones are.",
		Errors: []int{http.StatusBadRequest},
	}), h.opsListUsers)
	passwordRoute(r, ops(huma.Operation{
		OperationID: "ops-create-user", Method: http.MethodPost, Path: "/ops/auth/users",
		Summary: "Create an account", Description: "As `orb dev`'s seed data does: the address can be marked verified.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.opsCreateUser, passwordField{"password", passwordDocFormat})
	route(r, ops(huma.Operation{
		OperationID: "ops-get-user", Method: http.MethodGet, Path: "/ops/auth/users/{id}",
		Summary: "Get an account", Description: "With its sessions, passkeys, linked providers, second factors and usable codes (without the codes: they arrive by email).",
		Errors: notFound,
	}), h.opsGetUser)
	route(r, ops(huma.Operation{
		OperationID: "ops-delete-user", Method: http.MethodDelete, Path: "/ops/auth/users/{id}",
		Summary: "Delete an account", Description: "What the owner's own deletion does, without their password: sessions and keys are revoked, providers unlinked, and the data removed after the retention period.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound, http.StatusConflict},
	}), h.opsDeleteUser)
	route(r, ops(huma.Operation{
		OperationID: "ops-verify-user-email", Method: http.MethodPost, Path: "/ops/auth/users/{id}/verify-email",
		Summary: "Mark an address verified", DefaultStatus: http.StatusNoContent, Errors: notFound,
	}), h.opsVerifyUserEmail)
	route(r, ops(huma.Operation{
		OperationID: "ops-ban-user", Method: http.MethodPost, Path: "/ops/auth/users/{id}/ban",
		Summary: "Ban an account", Description: "The account can't sign in; its sessions and API keys are revoked at once.",
		DefaultStatus: http.StatusNoContent, Errors: notFound,
	}), h.opsBanUser)
	route(r, ops(huma.Operation{
		OperationID: "ops-unban-user", Method: http.MethodPost, Path: "/ops/auth/users/{id}/unban",
		Summary: "Lift a ban", DefaultStatus: http.StatusNoContent, Errors: notFound,
	}), h.opsUnbanUser)
	route(r, ops(huma.Operation{
		OperationID: "ops-grant-user-role", Method: http.MethodPost, Path: "/ops/auth/users/{id}/roles",
		Summary: "Grant a platform role", Description: "The address must be verified.",
		Errors: []int{http.StatusNotFound, http.StatusForbidden, http.StatusUnprocessableEntity},
	}), h.opsGrantRole)
	route(r, ops(huma.Operation{
		OperationID: "ops-revoke-user-role", Method: http.MethodDelete, Path: "/ops/auth/users/{id}/roles/{role}",
		Summary: "Revoke a platform role", Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity},
	}), h.opsRevokeRole)
	route(r, ops(huma.Operation{
		OperationID: "ops-revoke-user-sessions", Method: http.MethodDelete, Path: "/ops/auth/users/{id}/sessions",
		Summary: "End every session of an account", Errors: notFound,
	}), h.opsRevokeUserSessions)
	route(r, ops(huma.Operation{
		OperationID: "ops-revoke-user-session", Method: http.MethodDelete, Path: "/ops/auth/users/{id}/sessions/{sessionId}",
		Summary: "End one session of an account", DefaultStatus: http.StatusNoContent, Errors: notFound,
	}), h.opsRevokeUserSession)
	route(r, ops(huma.Operation{
		OperationID: "ops-remove-user-passkey", Method: http.MethodDelete, Path: "/ops/auth/users/{id}/passkeys/{passkeyId}",
		Summary: "Remove a passkey from an account", DefaultStatus: http.StatusNoContent, Errors: notFound,
	}), h.opsRemoveUserPasskey)
	route(r, ops(huma.Operation{
		OperationID: "ops-remove-user-identity", Method: http.MethodDelete, Path: "/ops/auth/users/{id}/identities/{identityId}",
		Summary: "Unlink a Google, Apple or GitHub account", DefaultStatus: http.StatusNoContent, Errors: notFound,
	}), h.opsRemoveUserIdentity)
	route(r, ops(huma.Operation{
		OperationID: "ops-enroll-user-totp", Method: http.MethodPost, Path: "/ops/auth/users/{id}/mfa/enroll",
		Summary: "Turn on an authenticator app for an account", Description: "Returns the secret and recovery codes once, as seed data does for the administrator.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusServiceUnavailable},
	}), h.opsEnrollUserTOTP)
	route(r, ops(huma.Operation{
		OperationID: "ops-reset-user-mfa", Method: http.MethodPost, Path: "/ops/auth/users/{id}/mfa/reset",
		Summary: "Reset an account's second factors", Description: "Removes the authenticator app, passkeys and recovery codes and ends every session, for a person locked out.",
		DefaultStatus: http.StatusNoContent, Errors: notFound,
	}), h.opsResetUserMFA)
	route(r, ops(huma.Operation{
		OperationID: "ops-impersonate-user", Method: http.MethodPost, Path: "/ops/auth/users/{id}/impersonate",
		Summary:       "Start a session as an account (development only)",
		Description:   "Available only while the app runs with the dev console (`orb dev`); production answers 403 `impersonation_off`. The session is audited as `auth.user.impersonated`.",
		DefaultStatus: http.StatusCreated, Errors: []int{http.StatusNotFound, http.StatusForbidden},
	}), h.opsImpersonateUser)
}

func opsUserResponse(u authdomain.User) OpsUserResponse {
	return OpsUserResponse{UserResponse: userResponse(u), BannedAt: u.BannedAt, BannedReason: u.BannedReason}
}

func (h *handler) opsListUsers(ctx context.Context, in *opsUserListInput) (*opsUserListOutput, error) {
	page, err := h.svc.ListUsers(ctx, authusecase.UserFilter{Query: in.Query, Cursor: in.Cursor, Limit: in.Limit})
	if err != nil {
		return nil, err
	}
	out := &opsUserListOutput{Body: OpsUserList{Users: make([]OpsUserResponse, len(page.Users)), NextCursor: page.NextCursor}}
	for i, u := range page.Users {
		out.Body.Users[i] = opsUserResponse(u)
	}
	return out, nil
}

func (h *handler) opsCreateUser(ctx context.Context, in *opsCreateUserInput) (*opsUserOutput, error) {
	u, err := h.svc.CreateUser(ctx, in.Body.Email, in.Body.Password, in.Body.EmailVerified)
	if refused, ok := errors.AsType[*authdomain.Refusal](err); ok {
		return nil, authError(refused) // the app's OnRegister hook; other errors keep v0.1's mappings
	}
	if err != nil {
		return nil, err
	}
	u, err = h.svc.User(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return &opsUserOutput{Body: opsUserResponse(u)}, nil
}

func (h *handler) opsGetUser(ctx context.Context, in *opsUserInput) (*opsUserDetailOutput, error) {
	d, err := h.svc.UserDetail(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	out := &opsUserDetailOutput{Body: OpsUserDetail{
		User: opsUserResponse(d.User), Sessions: make([]SessionResponse, len(d.Sessions)), Passkeys: make([]PasskeyResponse, len(d.Passkeys)),
		Identities: make([]IdentityResponse, len(d.Identities)), MFA: OpsMFAStatus(d.MFA), Codes: make([]OpsCodeResponse, len(d.Codes)),
	}}
	for i, s := range d.Sessions {
		out.Body.Sessions[i] = sessionResponse(s, false)
	}
	for i, p := range d.Passkeys {
		out.Body.Passkeys[i] = passkeyResponse(p)
	}
	for i, id := range d.Identities {
		out.Body.Identities[i] = identityResponse(id)
	}
	for i, c := range d.Codes {
		out.Body.Codes[i] = OpsCodeResponse{ID: c.ID, Purpose: c.Purpose, Attempts: c.Attempts, MaxAttempts: c.MaxAttempts, CreatedAt: c.CreatedAt, ExpiresAt: c.ExpiresAt}
	}
	return out, nil
}

func (h *handler) opsDeleteUser(ctx context.Context, in *opsUserInput) (*struct{}, error) {
	return nil, h.svc.DeleteUser(ctx, in.ID)
}

func (h *handler) opsVerifyUserEmail(ctx context.Context, in *opsUserInput) (*struct{}, error) {
	return nil, h.svc.VerifyUserEmail(ctx, in.ID)
}

func (h *handler) opsBanUser(ctx context.Context, in *opsBanInput) (*struct{}, error) {
	return nil, h.svc.BanUser(ctx, in.ID, in.Body.Reason)
}

func (h *handler) opsUnbanUser(ctx context.Context, in *opsUserInput) (*struct{}, error) {
	return nil, h.svc.UnbanUser(ctx, in.ID)
}

func (h *handler) opsGrantRole(ctx context.Context, in *opsRoleInput) (*opsUserOutput, error) {
	if err := h.svc.AuthorizeOps(ctx, authusecase.PermOpsAuthWrite); err != nil {
		return nil, err
	}
	if err := h.svc.GrantRole(ctx, in.ID, in.Body.Role); err != nil {
		return nil, err
	}
	u, err := h.svc.User(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &opsUserOutput{Body: opsUserResponse(u)}, nil
}

func (h *handler) opsRevokeRole(ctx context.Context, in *opsRolePathInput) (*opsUserOutput, error) {
	if err := h.svc.AuthorizeOps(ctx, authusecase.PermOpsAuthWrite); err != nil {
		return nil, err
	}
	if err := h.svc.RevokeRole(ctx, in.ID, in.Role); err != nil {
		return nil, err
	}
	u, err := h.svc.User(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &opsUserOutput{Body: opsUserResponse(u)}, nil
}

func (h *handler) opsRevokeUserSessions(ctx context.Context, in *opsUserInput) (*opsRevokedOutput, error) {
	n, err := h.svc.RevokeUserSessions(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &opsRevokedOutput{Body: OpsRevokedResponse{Revoked: n}}, nil
}

func (h *handler) opsRevokeUserSession(ctx context.Context, in *opsSessionInput) (*struct{}, error) {
	return nil, h.svc.RevokeUserSession(ctx, in.ID, in.SessionID)
}

func (h *handler) opsRemoveUserPasskey(ctx context.Context, in *opsPasskeyInput) (*struct{}, error) {
	return nil, h.svc.RemoveUserPasskey(ctx, in.ID, in.PasskeyID)
}

func (h *handler) opsRemoveUserIdentity(ctx context.Context, in *opsIdentityInput) (*struct{}, error) {
	return nil, h.svc.RemoveUserIdentity(ctx, in.ID, in.IdentityID)
}

func (h *handler) opsEnrollUserTOTP(ctx context.Context, in *opsUserInput) (*opsTOTPEnrollmentOutput, error) {
	if err := h.svc.AuthorizeOps(ctx, authusecase.PermOpsAuthWrite); err != nil {
		return nil, err
	}
	enrollment, codes, err := h.svc.EnrollTOTP(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &opsTOTPEnrollmentOutput{Body: OpsTOTPEnrollmentResponse{Secret: enrollment.Secret, URI: enrollment.URI, RecoveryCodes: codes}}, nil
}

func (h *handler) opsResetUserMFA(ctx context.Context, in *opsUserInput) (*struct{}, error) {
	if err := h.svc.AuthorizeOps(ctx, authusecase.PermOpsAuthWrite); err != nil {
		return nil, err
	}
	return nil, h.svc.ResetMFA(ctx, in.ID)
}

func (h *handler) opsImpersonateUser(ctx context.Context, in *opsImpersonateInput) (*opsImpersonateOutput, error) {
	res, err := h.svc.Impersonate(ctx, in.ID, in.Body.MFAVerified)
	if err != nil {
		return nil, err
	}
	return &opsImpersonateOutput{Body: OpsImpersonationResponse{
		Token: res.Token, Session: sessionResponse(res.Session, false), User: opsUserResponse(res.User), MFAVerified: in.Body.MFAVerified,
	}}, nil
}
