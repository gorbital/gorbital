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

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/openapi"

	authdomain "example.com/plateful/internal/modules/auth/domain"
	authusecase "example.com/plateful/internal/modules/auth/usecase"
)

// UserResponse is an account.
type UserResponse struct {
	ID            string    `json:"id" example:"usr_mfrggzdfmztwq2lk"`
	Email         string    `json:"email" example:"ada@example.com"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
	Roles         []string  `json:"roles" doc:"Platform roles, such as platform_admin"`
	HasPassword   bool      `json:"has_password" doc:"The account has a password; accounts created with Google, Apple or GitHub don't, until they reset one"`
}

// SessionResponse is a signed-in device.
type SessionResponse struct {
	ID          string    `json:"id" example:"ses_nbswy3dpeb3w64tmmq"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	ExpiresAt   time.Time `json:"expires_at" doc:"When the session ends unless used again"`
	IP          string    `json:"ip,omitempty"`
	UserAgent   string    `json:"user_agent,omitempty"`
	Current     bool      `json:"current" doc:"The session making this request"`
	MFAVerified bool      `json:"mfa_verified" doc:"Signed in or confirmed with a second factor"`
}

// LoginResponse is a new session (200), or a sign-in waiting for a second
// factor (202).
type LoginResponse struct {
	User    *UserResponse    `json:"user,omitempty" doc:"With a new session"`
	Session *SessionResponse `json:"session,omitempty" doc:"With a new session"`
	Token   string           `json:"token,omitempty" doc:"Only with transport bearer: send it as Authorization: Bearer <token>. It is shown once."`
	MFA     *MFAChallenge    `json:"mfa,omitempty" doc:"With status 202: the account has two-factor authentication; finish with POST /v1/auth/login/mfa"`
}

// MFAChallenge is a sign-in waiting for a second factor. Google and Apple
// web sign-ins put the same fields in the redirect's fragment.
type MFAChallenge struct {
	ChallengeToken string    `json:"challenge_token" doc:"Send it to POST /v1/auth/login/mfa. It is shown once."`
	Methods        []string  `json:"methods" doc:"Second factors accepted: totp (a code from the authenticator app), passkey or recovery_code"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// MeResponse is the signed-in user.
type MeResponse struct {
	User              UserResponse    `json:"user"`
	Session           SessionResponse `json:"session"`
	Permissions       []string        `json:"permissions" doc:"Granted by the user's roles to this session"`
	StepUpPermissions []string        `json:"step_up_permissions" doc:"Granted by the user's roles after signing in with a second factor"`
	MFAEnabled        bool            `json:"mfa_enabled" doc:"Two-factor authentication is on"`
	MFARequired       bool            `json:"mfa_required" doc:"A role of the account requires two-factor authentication"`
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

// LoginOutput is a sign-in's response: a session (200) or a second-factor
// challenge (202).
type LoginOutput struct {
	Status    int
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
		_            struct{}       `json:"-" additionalProperties:"true"`
		Password     string         `json:"password" maxLength:"512"`
		Code         string         `json:"code,omitempty" maxLength:"16" example:"123456" doc:"With two-factor authentication on: a code from the authenticator app"`
		RecoveryCode string         `json:"recovery_code,omitempty" maxLength:"32" example:"abcd-efgh-ijkl-mnop" doc:"With two-factor authentication on, instead of code"`
		Passkey      *PasskeyFactor `json:"passkey,omitempty" doc:"With two-factor authentication on, a passkey's response instead of code"`
	}
}

type sessionIDInput struct {
	ID string `path:"id" maxLength:"64" example:"ses_nbswy3dpeb3w64tmmq"`
}

type handler struct {
	svc    *authusecase.Service
	cookie string
}

// Config is how authhttp's options change the operations Register adds.
// The zero Config (with Cookie) registers v0.1's operations.
type Config struct {
	// Cookie names the session cookie browsers receive.
	Cookie string
	// Middleware runs on every operation under /v1/auth/, after the
	// module's and before the guards (authhttp's RouteMiddleware).
	Middleware []func(http.Handler) http.Handler
	// Registration registers POST /v1/auth/register; nil leaves it out
	// (authhttp's WithoutRegistration). DefaultRegistration is v0.1's.
	Registration Registration
	// MinPasswordLength is the shortest password the app accepts
	// (authhttp's MinPasswordLength). 0, or auth.MinPasswordLength, leaves
	// v0.1's wording, so the document stays v0.1's byte for byte; a higher
	// one is stated instead of 12 wherever a password field documents the
	// minimum.
	MinPasswordLength int
}

// Register adds the authentication operations to r, with v0.1's operation
// IDs, paths and documentation. A nil svc registers the operations without
// their dependencies, for exporting the OpenAPI document.
func Register(router *gorbital.Router, svc *authusecase.Service, c Config) {
	h := &handler{svc: svc, cookie: c.Cookie}
	r := routesOn(router)
	if c.MinPasswordLength > authlib.MinPasswordLength {
		r.minPassword = c.MinPasswordLength
	}
	if len(c.Middleware) > 0 {
		r.signIn = router.Group("", gorbital.Use(c.Middleware...))
	}
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

	if c.Registration != nil {
		c.Registration(r, h)
	}
	route(r, public(huma.Operation{
		OperationID: "auth-verify-email", Method: http.MethodPost, Path: "/v1/auth/verify-email",
		Summary: "Verify an email address with its code",
		Description: "Each code allows 5 attempts, and each address `auth.code_attempts` a day across codes (429). Verifying signs out every device and removes any passkey, " +
			"authenticator app or Google, Apple or GitHub link added before the address was proven.",
		DefaultStatus: http.StatusNoContent, Errors: limited,
	}), h.verify)
	route(r, public(huma.Operation{
		OperationID: "auth-resend-verification", Method: http.MethodPost, Path: "/v1/auth/verify-email/resend",
		Summary: "Send a new verification code", Description: "At most once a minute per address.",
		DefaultStatus: http.StatusAccepted, Errors: limited,
	}), h.resend)
	route(r, public(huma.Operation{
		OperationID: "auth-login", Method: http.MethodPost, Path: "/v1/auth/login",
		Summary: "Sign in",
		Description: "Starts a session (200). Browsers get an HttpOnly `__Host-session` cookie; native apps pass `\"transport\": \"bearer\"` and get the token in the response. " +
			"For an account with two-factor authentication the response is 202 with `mfa.challenge_token` instead: finish with `POST /v1/auth/login/mfa`.",
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}), h.login)
	route(r, public(huma.Operation{
		OperationID: "auth-forgot-password", Method: http.MethodPost, Path: "/v1/auth/password/forgot",
		Summary:       "Email a password reset code",
		Description:   "The response is the same whether or not the address has an account.",
		DefaultStatus: http.StatusAccepted, Errors: limited,
	}), h.forgot)
	passwordRoute(r, public(huma.Operation{
		OperationID: "auth-reset-password", Method: http.MethodPost, Path: "/v1/auth/password/reset",
		Summary: "Set a new password with a reset code",
		Description: "Signs out every device. Two-factor authentication stays on, except on an account whose address wasn't verified yet: the code verifies it, " +
			"as `POST /v1/auth/verify-email` does. Codes share the limit of `auth.code_attempts` a day per address (429).",
		DefaultStatus: http.StatusNoContent, Errors: append(limited, http.StatusServiceUnavailable),
	}), h.reset, passwordField{"password", newPasswordDocFormat})

	route(r, signedIn(huma.Operation{
		OperationID: "auth-me", Method: http.MethodGet, Path: "/v1/auth/me",
		Summary: "Get the signed-in user",
	}), h.me)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-logout", Method: http.MethodPost, Path: "/v1/auth/logout",
		Summary: "Sign out this device", DefaultStatus: http.StatusNoContent,
	}), h.logout)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-logout-all", Method: http.MethodPost, Path: "/v1/auth/logout-all",
		Summary: "Sign out every device",
	}), h.logoutAll)
	passwordRoute(r, signedIn(huma.Operation{
		OperationID: "auth-change-password", Method: http.MethodPut, Path: "/v1/auth/password",
		Summary: "Change the password", Description: "Signs out other devices.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnprocessableEntity},
	}), h.changePassword, passwordField{"new_password", passwordDocFormat})
	route(r, signedIn(huma.Operation{
		OperationID: "auth-list-sessions", Method: http.MethodGet, Path: "/v1/auth/sessions",
		Summary: "List signed-in devices",
	}), h.sessions)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-revoke-session", Method: http.MethodDelete, Path: "/v1/auth/sessions/{id}",
		Summary: "Sign out one device", DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}), h.revokeSession)
	route(r, signedIn(huma.Operation{
		OperationID: "auth-delete-account", Method: http.MethodDelete, Path: "/v1/auth/me",
		Summary:       "Delete the account",
		Description:   "Requires the password, and with two-factor authentication on a code, recovery code or passkey response (start one with `POST /v1/auth/passkeys/verification`). Signs out every device.",
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusServiceUnavailable},
	}), h.deleteAccount)

	registerMFA(r, h, public, signedIn)
	registerPasskeys(r, h, public, signedIn)
	registerSocial(r, h, public, signedIn)
	registerAPIKeys(r, h, signedIn)
	registerOpsUsers(r, h)
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

func (h *handler) login(ctx context.Context, in *loginInput) (*LoginOutput, error) {
	res, err := h.svc.Login(ctx, in.Body.Email, in.Body.Password)
	if err != nil {
		return nil, authError(err)
	}
	return h.signedIn(res, in.Body.Transport), nil
}

// session is the response for a new session: the token in a cookie, or in
// the body for transport bearer.
func (h *handler) session(res authusecase.LoginResult, transport string) *LoginOutput {
	user, session := userResponse(res.User), sessionResponse(res.Session, true)
	out := &LoginOutput{Status: http.StatusOK, Body: LoginResponse{User: &user, Session: &session}}
	if transport == authdomain.TransportBearer {
		out.Body.Token = res.Token
	} else {
		out.SetCookie = []http.Cookie{*authlib.SessionCookie(h.cookie, res.Token, res.Session.AbsoluteExpiresAt)}
	}
	return out
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
	return &meOutput{Body: MeResponse{
		User: userResponse(view.User), Session: sessionResponse(view.Session, true),
		Permissions: orEmpty(view.Permissions), StepUpPermissions: orEmpty(view.StepUp),
		MFAEnabled: view.MFAEnabled, MFARequired: view.MFARequired,
	}}, nil
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
	factor, err := secondFactor(in.Body.Code, in.Body.RecoveryCode, in.Body.Passkey)
	if err != nil {
		return nil, err
	}
	if err := h.svc.DeleteAccount(ctx, in.Body.Password, factor); err != nil {
		return nil, authError(err)
	}
	return &cookieOutput{SetCookie: []http.Cookie{*authlib.ClearSessionCookie(h.cookie)}}, nil
}

// authError is AuthError.
func authError(err error) error { return AuthError(err) }

// AuthError adds the reason to a rejected password and the wait to a rate
// limit, and answers an app hook's refusal with its code and 403; other
// errors are mapped by the module's Errors (authhttp's module.go).
func AuthError(err error) error {
	var weak *authlib.PasswordError
	var limited *authdomain.RateLimitError
	var refused *authdomain.Refusal
	switch {
	case errors.As(err, &weak):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "weak_password", "the password "+weak.Reason)
	case errors.As(err, &limited):
		seconds := max(int(limited.RetryAfter.Round(time.Second)/time.Second), 1)
		return httpx.NewProblem(http.StatusTooManyRequests, "too_many_attempts", fmt.Sprintf("too many attempts; try again in %d seconds", seconds))
	case errors.As(err, &refused):
		return httpx.NewProblem(http.StatusForbidden, refused.Code, refused.Detail)
	}
	return err
}

func userResponse(u authdomain.User) UserResponse {
	return UserResponse{ID: u.ID, Email: u.Email, EmailVerified: u.EmailVerified(), CreatedAt: u.CreatedAt, Roles: orEmpty(u.Roles), HasPassword: u.HasPassword()}
}

func sessionResponse(s authdomain.Session, current bool) SessionResponse {
	return SessionResponse{
		ID: s.ID, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, ExpiresAt: s.ExpiresAt(),
		IP: s.IP, UserAgent: s.UserAgent, Current: current, MFAVerified: s.MFAVerified(),
	}
}

// orEmpty returns a non-nil slice, so JSON has [] instead of null.
func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
