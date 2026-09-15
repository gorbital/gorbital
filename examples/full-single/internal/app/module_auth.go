package app

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/httpx"
	authlib "apistock.dev/modules/auth"

	authmodule "example.com/acme-api/internal/modules/auth"
	authdomain "example.com/acme-api/internal/modules/auth/domain"
)

// registerAuth wires the authentication module. Error codes are public API:
// add new ones, never change existing ones.
func registerAuth(api huma.API, mapper *httpx.Mapper, m *authmodule.Module) error {
	err := mapper.Add(
		httpx.Mapping{Err: authlib.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		httpx.Mapping{Err: authdomain.ErrActorRequired, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		httpx.Mapping{Err: authlib.ErrInvalidEmail, Status: http.StatusUnprocessableEntity, Code: "invalid_email", Detail: "the email address is not valid"},
		httpx.Mapping{Err: authlib.ErrWeakPassword, Status: http.StatusUnprocessableEntity, Code: "weak_password", Detail: "the password does not meet the password policy"},
		httpx.Mapping{Err: authdomain.ErrInvalidCredentials, Status: http.StatusUnauthorized, Code: "invalid_credentials", Detail: "the email address or password is wrong"},
		httpx.Mapping{Err: authdomain.ErrEmailNotVerified, Status: http.StatusForbidden, Code: "email_not_verified", Detail: "verify your email address before signing in"},
		httpx.Mapping{Err: authdomain.ErrInvalidCode, Status: http.StatusUnprocessableEntity, Code: "invalid_code", Detail: "the code is wrong, used or expired"},
		httpx.Mapping{Err: authdomain.ErrTooManyAttempts, Status: http.StatusTooManyRequests, Code: "too_many_attempts", Detail: "too many attempts; try again later"},
		httpx.Mapping{Err: authdomain.ErrSessionNotFound, Status: http.StatusNotFound, Code: "session_not_found", Detail: "no active session of yours has this ID"},
		httpx.Mapping{Err: authdomain.ErrInvalidMFA, Status: http.StatusUnauthorized, Code: "invalid_mfa", Detail: "the second factor is wrong or already used, or the sign-in expired"},
		httpx.Mapping{Err: authdomain.ErrMFAAlreadyEnabled, Status: http.StatusConflict, Code: "mfa_already_enabled", Detail: "two-factor authentication is already on"},
		httpx.Mapping{Err: authdomain.ErrMFANotEnabled, Status: http.StatusConflict, Code: "mfa_not_enabled", Detail: "two-factor authentication isn't on, or its setup wasn't started"},
		httpx.Mapping{Err: authdomain.ErrMFARequiredByRole, Status: http.StatusConflict, Code: "mfa_required_by_role", Detail: "a role of this account requires two-factor authentication"},
		httpx.Mapping{Err: authdomain.ErrMFAUnavailable, Status: http.StatusServiceUnavailable, Code: "mfa_unavailable", Detail: "two-factor authentication isn't configured on this server"},
	)
	if err != nil {
		return fmt.Errorf("auth module: %w", err)
	}
	m.Register(api)
	return nil
}
