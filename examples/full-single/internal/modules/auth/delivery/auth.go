// Package delivery is the auth module's HTTP adapter: Huma operations and
// request and response types for /v1/auth.
package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/httpx"
	authlib "apistock.dev/modules/auth"
	"apistock.dev/modules/openapi"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
	authusecase "example.com/acme-api/internal/modules/auth/usecase"
)

// UserResponse is an account.
type UserResponse struct {
	ID            string    `json:"id" example:"usr_mfrggzdfmztwq2lk"`
	Email         string    `json:"email" example:"ada@example.com"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	Roles         []string  `json:"roles" doc:"Platform roles, such as platform_admin"`
}

// SessionResponse is a signed-in device.
type SessionResponse struct {
	ID         string    `json:"id" example:"ses_nbswy3dpeb3w64tmmq"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at" doc:"When the session ends unless used again"`
	IP         string    `json:"ip,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"`
	Current    bool      `json:"current" doc:"The session making this request"`
}

// LoginResponse is a new session.
type LoginResponse struct {
	User    UserResponse    `json:"user"`
	Session SessionResponse `json:"session"`
	Token   string          `json:"token,omitempty" doc:"Only with transport bearer: send it as Authorization: Bearer <token>. It is shown once."`
}

// MeResponse is the signed-in user.
type MeResponse struct {
	User        UserResponse    `json:"user"`
	Session     SessionResponse `json:"session"`
	Permissions []string        `json:"permissions" doc:"Granted by the user's roles"`
}

// AcceptedResponse confirms a request whose result arrives by email.
type AcceptedResponse struct {
	Status  string `json:"status" enum:"check_your_email"`
	Message string `json:"message"`
}

// SessionList is the user's active sessions, most recently used first.
type SessionList struct {
	Sessions []SessionResponse `json:"sessions"`
}

// LogoutAllResponse counts ended sessions.
type LogoutAllResponse struct {
	Revoked int64 `json:"revoked"`
}

type acceptedOutput struct{ Body AcceptedResponse }

type loginOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      LoginResponse
}

type cookieOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
}

type logoutAllOutput struct {
	SetCookie []http.Cookie `header:"Set-Cookie"`
	Body      LogoutAllResponse
}

type meOutput struct{ Body MeResponse }

type sessionListOutput struct{ Body SessionList }

type registerInput struct {
	Body struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Email    string   `json:"email" maxLength:"254" example:"ada@example.com"`
		Password string   `json:"password" maxLength:"512" doc:"At least 12 characters"`
	}
}

type loginInput struct {
	Body struct {
		_         struct{} `json:"-" additionalProperties:"true"`
		Email     string   `json:"email" maxLength:"254" example:"ada@example.com"`
		Password  string   `json:"password" maxLength:"512"`
		Transport string   `json:"transport,omitempty" enum:"cookie,bearer" default:"cookie" doc:"cookie (browsers): an HttpOnly session cookie; bearer (native apps): the token in the response"`
	}
}

type emailInput struct {
	Body struct {
		_     struct{} `json:"-" additionalProperties:"true"`
		Email string   `json:"email" maxLength:"254" example:"ada@example.com"`
	}
}

type verifyInput struct {
	Body struct {
		_     struct{} `json:"-" additionalProperties:"true"`
		Email string   `json:"email" maxLength:"254" example:"ada@example.com"`
		Code  string   `json:"code" maxLength:"16" example:"123456"`
	}
}

type resetInput struct {
	Body struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Email    string   `json:"email" maxLength:"254" example:"ada@example.com"`
		Code     string   `json:"code" maxLength:"16" example:"123456"`
		Password string   `json:"password" maxLength:"512" doc:"The new password, at least 12 characters"`
	}
}

type changePasswordInput struct {
	Body struct {
		_               struct{} `json:"-" additionalProperties:"true"`
		CurrentPassword string   `json:"current_password" maxLength:"512"`
		NewPassword     string   `json:"new_password" maxLength:"512" doc:"At least 12 characters"`
	}
}

type deleteAccountInput struct {
	Body struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Password string   `json:"password" maxLength:"512"`
	}
}

type sessionIDInput struct {
	ID string `path:"id" maxLength:"64" example:"ses_nbswy3dpeb3w64tmmq"`
}

type handler struct {
	svc    *authusecase.Service
	cookie string
}

// Register adds the authentication operations to api. Browsers receive the
// session in the cookie named cookie.
func Register(api huma.API, svc *authusecase.Service, cookie string) {
	h := &handler{svc: svc, cookie: cookie}
	public := func(op huma.Operation) huma.Operation {
		op.Tags = []string{"Auth"}
		return op
	}
	signedIn := func(op huma.Operation) huma.Operation {
		op.Tags, op.Security = []string{"Auth"}, openapi.Bearer
		op.Errors = append([]int{http.StatusUnauthorized}, op.Errors...)
		return op
	}
	limited := []int{http.StatusUnprocessableEntity, http.StatusTooManyRequests}

	huma.Register(api, public(huma.Operation{
		OperationID: "auth-register", Method: http.MethodPost, Path: "/v1/auth/register",
		Summary:       "Create an account",
		Description:   "Emails a 6-digit verification code. The response is the same whether or not the address already has an account.",
		DefaultStatus: http.StatusAccepted, Errors: limited,
	}), h.register)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-verify-email", Method: http.MethodPost, Path: "/v1/auth/verify-email",
		Summary: "Verify an email address with its code", DefaultStatus: http.StatusNoContent, Errors: limited,
	}), h.verify)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-resend-verification", Method: http.MethodPost, Path: "/v1/auth/verify-email/resend",
		Summary: "Send a new verification code", Description: "At most once a minute per address.",
		DefaultStatus: http.StatusAccepted, Errors: limited,
	}), h.resend)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-login", Method: http.MethodPost, Path: "/v1/auth/login",
		Summary:     "Sign in",
		Description: "Starts a session. Browsers get an HttpOnly `__Host-session` cookie; native apps pass `\"transport\": \"bearer\"` and get the token in the response.",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}), h.login)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-forgot-password", Method: http.MethodPost, Path: "/v1/auth/password/forgot",
		Summary:       "Email a password reset code",
		Description:   "The response is the same whether or not the address has an account.",
		DefaultStatus: http.StatusAccepted, Errors: limited,
	}), h.forgot)
	huma.Register(api, public(huma.Operation{
		OperationID: "auth-reset-password", Method: http.MethodPost, Path: "/v1/auth/password/reset",
		Summary: "Set a new password with a reset code", Description: "Signs out every device.",
		DefaultStatus: http.StatusNoContent, Errors: limited,
	}), h.reset)

	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-me", Method: http.MethodGet, Path: "/v1/auth/me",
		Summary: "Get the signed-in user",
	}), h.me)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-logout", Method: http.MethodPost, Path: "/v1/auth/logout",
		Summary: "Sign out this device", DefaultStatus: http.StatusNoContent,
	}), h.logout)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-logout-all", Method: http.MethodPost, Path: "/v1/auth/logout-all",
		Summary: "Sign out every device",
	}), h.logoutAll)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-change-password", Method: http.MethodPut, Path: "/v1/auth/password",
		Summary: "Change the password", Description: "Signs out other devices.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnprocessableEntity},
	}), h.changePassword)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-list-sessions", Method: http.MethodGet, Path: "/v1/auth/sessions",
		Summary: "List signed-in devices",
	}), h.sessions)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-revoke-session", Method: http.MethodDelete, Path: "/v1/auth/sessions/{id}",
		Summary: "Sign out one device", DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}), h.revokeSession)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "auth-delete-account", Method: http.MethodDelete, Path: "/v1/auth/me",
		Summary: "Delete the account", Description: "Requires the password. Signs out every device.",
		DefaultStatus: http.StatusNoContent,
	}), h.deleteAccount)
}

func (h *handler) register(ctx context.Context, in *registerInput) (*acceptedOutput, error) {
	if err := h.svc.Register(ctx, in.Body.Email, in.Body.Password); err != nil {
		return nil, authError(err)
	}
	return &acceptedOutput{Body: AcceptedResponse{Status: "check_your_email", Message: "Check your email for a 6-digit code to verify your address."}}, nil
}

func (h *handler) verify(ctx context.Context, in *verifyInput) (*struct{}, error) {
	return nil, authError(h.svc.VerifyEmail(ctx, in.Body.Email, in.Body.Code))
}

func (h *handler) resend(ctx context.Context, in *emailInput) (*acceptedOutput, error) {
	if err := h.svc.ResendVerification(ctx, in.Body.Email); err != nil {
		return nil, authError(err)
	}
	return &acceptedOutput{Body: AcceptedResponse{Status: "check_your_email", Message: "If the address is waiting for verification, a new code is on its way."}}, nil
}

func (h *handler) login(ctx context.Context, in *loginInput) (*loginOutput, error) {
	res, err := h.svc.Login(ctx, in.Body.Email, in.Body.Password)
	if err != nil {
		return nil, authError(err)
	}
	out := &loginOutput{Body: LoginResponse{User: userResponse(res.User), Session: sessionResponse(res.Session, true)}}
	if in.Body.Transport == authdomain.TransportBearer {
		out.Body.Token = res.Token
	} else {
		out.SetCookie = []http.Cookie{*authlib.SessionCookie(h.cookie, res.Token, res.Session.AbsoluteExpiresAt)}
	}
	return out, nil
}

func (h *handler) forgot(ctx context.Context, in *emailInput) (*acceptedOutput, error) {
	if err := h.svc.RequestPasswordReset(ctx, in.Body.Email); err != nil {
		return nil, authError(err)
	}
	return &acceptedOutput{Body: AcceptedResponse{Status: "check_your_email", Message: "If the address has an account, a reset code is on its way."}}, nil
}

func (h *handler) reset(ctx context.Context, in *resetInput) (*struct{}, error) {
	return nil, authError(h.svc.ResetPassword(ctx, in.Body.Email, in.Body.Code, in.Body.Password))
}

func (h *handler) me(ctx context.Context, _ *struct{}) (*meOutput, error) {
	view, err := h.svc.Me(ctx)
	if err != nil {
		return nil, err
	}
	perms := view.Permissions
	if perms == nil {
		perms = []string{}
	}
	return &meOutput{Body: MeResponse{User: userResponse(view.User), Session: sessionResponse(view.Session, true), Permissions: perms}}, nil
}

func (h *handler) logout(ctx context.Context, _ *struct{}) (*cookieOutput, error) {
	if err := h.svc.Logout(ctx); err != nil {
		return nil, err
	}
	return &cookieOutput{SetCookie: []http.Cookie{*authlib.ClearSessionCookie(h.cookie)}}, nil
}

func (h *handler) logoutAll(ctx context.Context, _ *struct{}) (*logoutAllOutput, error) {
	n, err := h.svc.LogoutAll(ctx)
	if err != nil {
		return nil, err
	}
	return &logoutAllOutput{SetCookie: []http.Cookie{*authlib.ClearSessionCookie(h.cookie)}, Body: LogoutAllResponse{Revoked: n}}, nil
}

func (h *handler) changePassword(ctx context.Context, in *changePasswordInput) (*struct{}, error) {
	return nil, authError(h.svc.ChangePassword(ctx, in.Body.CurrentPassword, in.Body.NewPassword))
}

func (h *handler) sessions(ctx context.Context, _ *struct{}) (*sessionListOutput, error) {
	views, err := h.svc.Sessions(ctx)
	if err != nil {
		return nil, err
	}
	out := &sessionListOutput{Body: SessionList{Sessions: make([]SessionResponse, len(views))}}
	for i, v := range views {
		out.Body.Sessions[i] = sessionResponse(v.Session, v.Current)
	}
	return out, nil
}

func (h *handler) revokeSession(ctx context.Context, in *sessionIDInput) (*struct{}, error) {
	return nil, h.svc.RevokeSession(ctx, in.ID)
}

func (h *handler) deleteAccount(ctx context.Context, in *deleteAccountInput) (*cookieOutput, error) {
	if err := h.svc.DeleteAccount(ctx, in.Body.Password); err != nil {
		return nil, err
	}
	return &cookieOutput{SetCookie: []http.Cookie{*authlib.ClearSessionCookie(h.cookie)}}, nil
}

// authError adds the reason to a rejected password and the wait to a rate
// limit; other errors are mapped in internal/app/module_auth.go.
func authError(err error) error {
	var weak *authlib.PasswordError
	var limited *authdomain.RateLimitError
	switch {
	case errors.As(err, &weak):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "weak_password", "the password "+weak.Reason)
	case errors.As(err, &limited):
		seconds := max(int(limited.RetryAfter.Round(time.Second)/time.Second), 1)
		return httpx.NewProblem(http.StatusTooManyRequests, "too_many_attempts", fmt.Sprintf("too many attempts; try again in %d seconds", seconds))
	}
	return err
}

func userResponse(u authdomain.User) UserResponse {
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	return UserResponse{ID: u.ID, Email: u.Email, EmailVerified: u.EmailVerified(), CreatedAt: u.CreatedAt, Roles: roles}
}

func sessionResponse(s authdomain.Session, current bool) SessionResponse {
	return SessionResponse{
		ID: s.ID, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, ExpiresAt: s.ExpiresAt(),
		IP: s.IP, UserAgent: s.UserAgent, Current: current,
	}
}
