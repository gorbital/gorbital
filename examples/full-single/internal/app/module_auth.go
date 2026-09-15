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
		httpx.Mapping{Err: authdomain.ErrInvalidPasskey, Status: http.StatusUnauthorized, Code: "invalid_passkey", Detail: "the passkey couldn't be verified, or the ceremony was used or expired"},
		httpx.Mapping{Err: authdomain.ErrPasskeyNotFound, Status: http.StatusNotFound, Code: "passkey_not_found", Detail: "no passkey of yours has this ID"},
		httpx.Mapping{Err: authdomain.ErrPasskeyLimitReached, Status: http.StatusConflict, Code: "passkey_limit_reached", Detail: "an account can have at most 10 passkeys"},
		httpx.Mapping{Err: authdomain.ErrInvalidPasskeyName, Status: http.StatusUnprocessableEntity, Code: "invalid_passkey_name", Detail: "a passkey name must be 1 to 100 characters"},
		httpx.Mapping{Err: authdomain.ErrPasskeysUnavailable, Status: http.StatusServiceUnavailable, Code: "passkeys_unavailable", Detail: "passkeys aren't configured on this server (WEBAUTHN_RP_ID)"},
		httpx.Mapping{Err: authdomain.ErrInvalidSocialToken, Status: http.StatusUnauthorized, Code: "invalid_social_token", Detail: "the sign-in with Google or Apple couldn't be verified; start again"},
		httpx.Mapping{Err: authdomain.ErrInvalidState, Status: http.StatusUnauthorized, Code: "invalid_state", Detail: "the sign-in expired or was started in another browser; start again"},
		httpx.Mapping{Err: authdomain.ErrSocialEmailUnverified, Status: http.StatusForbidden, Code: "social_email_unverified", Detail: "the provider hasn't verified this email address"},
		httpx.Mapping{Err: authdomain.ErrInvalidReturnTo, Status: http.StatusUnprocessableEntity, Code: "invalid_return_to", Detail: "return_to must be an absolute URL on the API's origin or APP_CORS_ORIGINS"},
		httpx.Mapping{Err: authdomain.ErrIdentityNotFound, Status: http.StatusNotFound, Code: "identity_not_found", Detail: "no linked account of yours has this ID"},
		httpx.Mapping{Err: authdomain.ErrLastSignInMethod, Status: http.StatusConflict, Code: "last_sign_in_method", Detail: "this is the account's last way to sign in; set a password or add a passkey first"},
		httpx.Mapping{Err: authdomain.ErrSocialUnavailable, Status: http.StatusServiceUnavailable, Code: "social_unavailable", Detail: "this sign-in provider isn't configured on this server (see AUTH_PROVIDERS.md)"},
	)
	if err != nil {
		return fmt.Errorf("auth module: %w", err)
	}
	m.Register(api)
	return nil
}
