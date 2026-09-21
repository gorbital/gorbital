package authhttp

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/settings"

	"example.com/shelfie/internal/modules/auth/delivery"
	"example.com/shelfie/internal/modules/auth/delivery/jobs/authcleanup"
	"example.com/shelfie/internal/modules/auth/delivery/jobs/authrevoke"
	authdomain "example.com/shelfie/internal/modules/auth/domain"
	"example.com/shelfie/internal/modules/auth/repository/migrations"
	"example.com/shelfie/internal/modules/auth/usecase"
)

// Platform roles of v0.1 apps (ADR-0038). Their names are public API.
const (
	rolePlatformAdmin = "platform_admin"
	roleOpsViewer     = "ops_viewer"
)

// Module returns sign-in as a gorbital module, which gorbital.New adds
// before the app's modules: its routes under /v1/auth/, /ops/auth/users and
// /ops/service-accounts, its error mappings, permissions, runtime settings
// (auth.*), the auth_cleanup and auth_revoke_tokens jobs, its rate limiters
// and the retention of deleted and unverified accounts (which the
// operations API lists in /ops/auth/rate-limits and /ops/retention), and its
// migrations under the versions v0.1 apps hold them under, so a v0.1
// database migrates as a no-op.
func (a *Authenticator) Module() gorbital.Module {
	return gorbital.Module{
		Name:         "auth",
		Errors:       errorMappings(),
		Permissions:  permissions(),
		Settings:     func(r *settings.Registry) { *a.settings = declareSettings(r, a.opts.apiKeyMaxTTL) },
		Jobs:         a.defineJobs,
		Migrations:   moduleMigrations(),
		RateLimiters: slices.Clone(limiters),
		Retention:    a.retention,
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			// svc is nil while the OpenAPI document is exported: the
			// operations are registered, but no use case runs.
			delivery.Register(r, a.service(), a.routes())
		},
	}
}

// routes are how the options change sign-in's operations.
func (a *Authenticator) routes() delivery.Config {
	c := delivery.Config{
		Cookie: authlib.DefaultCookieName, Middleware: a.opts.routeMiddleware,
		Registration: a.opts.registration, MinPasswordLength: a.opts.minPasswordLength,
	}
	if t := a.signInTester(); t != nil {
		// Test round trips return to the real callbacks; they are answered
		// before anything else sees them (ADR-0087).
		c.Middleware = append([]func(http.Handler) http.Handler{t.Callbacks}, c.Middleware...)
	}
	switch {
	case a.opts.closed:
		c.Registration = nil
	case c.Registration == nil:
		c.Registration = delivery.DefaultRegistration
	}
	return c
}

// moduleMigrations are sign-in's migrations with the versions v0.1 apps
// carry them under in db/migrations, byte for byte (ADR-0083). Released
// versions never change; add new migrations with later versions.
func moduleMigrations() []gorbital.Migration {
	return []gorbital.Migration{
		{Version: 20260915000001, Name: "auth", FS: migrations.FS, File: "00001_auth.sql"},
		{Version: 20260915000004, Name: "auth_mfa", FS: migrations.FS, File: "00002_auth_mfa.sql"},
		{Version: 20260915000005, Name: "auth_passkeys", FS: migrations.FS, File: "00003_auth_passkeys.sql"},
		{Version: 20260915000006, Name: "auth_social", FS: migrations.FS, File: "00004_auth_social.sql"},
		{Version: 20260917000001, Name: "auth_token_revocations", FS: migrations.FS, File: "00005_auth_token_revocations.sql"},
		{Version: 20260918000020, Name: "auth_api_keys", FS: migrations.FS, File: "00006_auth_api_keys.sql"},
		{Version: 20260918000030, Name: "auth_github", FS: migrations.FS, File: "00007_auth_github.sql"},
		{Version: 20260918000070, Name: "auth_bans", FS: migrations.FS, File: "00008_auth_bans.sql"},
	}
}

// permissions are the permissions sign-in checks, with the roles of v0.1
// apps that hold them. The ops roles grant them only to sessions signed in
// with a second factor (Setup), so never to API keys (ADR-0058).
//
// ops.auth.read and ops.auth.write are sign-in's: besides /ops/auth/users,
// the operations API (gorbital.dev/gorbital/opshttp) checks them for
// /ops/auth/providers and /ops/auth/rate-limits, and doesn't declare them,
// so an app with both declares each once.
func permissions() []gorbital.Permission {
	return []gorbital.Permission{
		{Name: usecase.PermOpsAuthRead, Description: "See which sign-in methods are configured", Roles: []string{rolePlatformAdmin, roleOpsViewer}},
		{Name: usecase.PermOpsAuthWrite, Description: "Manage accounts: create, ban, delete, end sessions, remove passkeys and links, reset second factors, impersonate in development", Roles: []string{rolePlatformAdmin}},
		{Name: usecase.PermServiceAccountsRead, Description: "See service accounts and their API keys", Roles: []string{rolePlatformAdmin, roleOpsViewer}},
		{Name: usecase.PermServiceAccountsWrite, Description: "Create, change and delete service accounts and their API keys", Roles: []string{rolePlatformAdmin}},
	}
}

// declareRoles declares the user role every account holds when no module
// grants it a permission, and requires a second factor for the ops roles,
// as a v0.1 app's permissions.go does, and for the roles of RequireMFA.
func declareRoles(catalog *authlib.Catalog, requireMFA []string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("authhttp: permission catalog: %v", r)
		}
	}()
	if !catalog.HasRole(usecase.RoleUser) {
		catalog.Role(usecase.RoleUser, "Held by every signed-in user without a grant; by API keys only within their scopes")
	}
	for _, role := range []string{rolePlatformAdmin, roleOpsViewer} {
		if catalog.HasRole(role) {
			catalog.RequireMFA(role)
		}
	}
	for _, role := range requireMFA {
		switch {
		case role == usecase.RoleUser:
			return fmt.Errorf("authhttp: RequireMFA(%q): every account holds the user role, which can't require a second factor", role)
		case !catalog.HasRole(role):
			return fmt.Errorf("authhttp: RequireMFA(%q): no module declares the role (gorbital.Module.Permissions)", role)
		}
		catalog.RequireMFA(role)
	}
	return nil
}

// retention is how long sign-in keeps deleted and unverified accounts, as a
// v0.1 app's /ops/retention lists it: the auth_cleanup job deletes both.
func (a *Authenticator) retention(gorbital.Deps) []gorbital.Retention {
	return []gorbital.Retention{
		{Data: "deleted_accounts", Setting: a.settings.deletedAccountRetention, Job: authcleanup.Name},
		{Data: "unverified_accounts", Setting: a.settings.unverifiedAccountTTL, Job: authcleanup.Name},
	}
}

// defineJobs defines auth_cleanup and auth_revoke_tokens with v0.1's
// defaults. Operators can override them in /ops/jobs/definitions.
func (a *Authenticator) defineJobs(defs *jobs.Definitions, d gorbital.Deps) {
	jobs.Define(defs, jobs.Definition[authcleanup.Args]{
		Name:        authcleanup.Name,
		Description: "Removes ended sessions, old email codes and accounts deleted longer ago than auth.deleted_account_retention.",
		Worker: authcleanup.NewWorker(func(ctx context.Context) (authdomain.CleanupResult, error) {
			return a.service().Cleanup(ctx)
		}, d.Logger),
		NewArgs:     func() authcleanup.Args { return authcleanup.Args{} },
		Enabled:     true,
		Schedule:    "30 3 * * *",
		Timeout:     5 * time.Minute,
		MaxAttempts: 3,
		Queue:       "default",
		Priority:    2,
	})
	jobs.Define(defs, jobs.Definition[authrevoke.Args]{
		Name:        authrevoke.Name,
		Description: "Revokes the Apple refresh tokens of unlinked identities and deleted accounts, retrying failures with backoff.",
		Worker: authrevoke.NewWorker(func(ctx context.Context) (authdomain.RevocationResult, error) {
			return a.service().RevokeProviderTokens(ctx)
		}, d.Logger),
		NewArgs:     func() authrevoke.Args { return authrevoke.Args{} },
		Enabled:     true,
		Schedule:    "@every 1m",
		Timeout:     5 * time.Minute,
		MaxAttempts: 1, // the next run retries; revocations keep their own backoff
		Queue:       "default",
		Priority:    2,
	})
}

// errorMappings map sign-in's errors to problem responses, as a v0.1 app's
// module_auth.go does. Error codes are public API: add new ones, never
// change existing ones.
func errorMappings() []httpx.Mapping {
	return []httpx.Mapping{
		{Err: authlib.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		{Err: authdomain.ErrActorRequired, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		{Err: authlib.ErrInvalidEmail, Status: http.StatusUnprocessableEntity, Code: "invalid_email", Detail: "the email address is not valid"},
		{Err: authlib.ErrWeakPassword, Status: http.StatusUnprocessableEntity, Code: "weak_password", Detail: "the password does not meet the password policy"},
		{Err: authdomain.ErrInvalidCredentials, Status: http.StatusUnauthorized, Code: "invalid_credentials", Detail: "the email address or password is wrong"},
		{Err: authdomain.ErrEmailNotVerified, Status: http.StatusForbidden, Code: "email_not_verified", Detail: "verify your email address before signing in"},
		{Err: authdomain.ErrInvalidCode, Status: http.StatusUnprocessableEntity, Code: "invalid_code", Detail: "the code is wrong, used or expired"},
		{Err: authdomain.ErrTooManyAttempts, Status: http.StatusTooManyRequests, Code: "too_many_attempts", Detail: "too many attempts; try again later"},
		{Err: authdomain.ErrSessionNotFound, Status: http.StatusNotFound, Code: "session_not_found", Detail: "no active session of yours has this ID"},
		{Err: authdomain.ErrInvalidMFA, Status: http.StatusUnauthorized, Code: "invalid_mfa", Detail: "the second factor is wrong or already used, or the sign-in expired"},
		{Err: authdomain.ErrMFAAlreadyEnabled, Status: http.StatusConflict, Code: "mfa_already_enabled", Detail: "two-factor authentication is already on"},
		{Err: authdomain.ErrMFANotEnabled, Status: http.StatusConflict, Code: "mfa_not_enabled", Detail: "two-factor authentication isn't on, or its setup wasn't started"},
		{Err: authdomain.ErrMFARequiredByRole, Status: http.StatusConflict, Code: "mfa_required_by_role", Detail: "a role of this account requires two-factor authentication"},
		{Err: authdomain.ErrMFAUnavailable, Status: http.StatusServiceUnavailable, Code: "mfa_unavailable", Detail: "two-factor authentication isn't configured on this server"},
		{Err: authdomain.ErrInvalidPasskey, Status: http.StatusUnauthorized, Code: "invalid_passkey", Detail: "the passkey couldn't be verified, or the ceremony was used or expired"},
		{Err: authdomain.ErrPasskeyNotFound, Status: http.StatusNotFound, Code: "passkey_not_found", Detail: "no passkey of yours has this ID"},
		{Err: authdomain.ErrPasskeyLimitReached, Status: http.StatusConflict, Code: "passkey_limit_reached", Detail: "an account can have at most 10 passkeys"},
		{Err: authdomain.ErrInvalidPasskeyName, Status: http.StatusUnprocessableEntity, Code: "invalid_passkey_name", Detail: "a passkey name must be 1 to 100 characters"},
		{Err: authdomain.ErrPasskeysUnavailable, Status: http.StatusServiceUnavailable, Code: "passkeys_unavailable", Detail: "passkeys aren't configured on this server (WEBAUTHN_RP_ID)"},
		{Err: authdomain.ErrInvalidSocialToken, Status: http.StatusUnauthorized, Code: "invalid_social_token", Detail: "the sign-in with Google, Apple or GitHub couldn't be verified; start again"},
		{Err: authdomain.ErrInvalidState, Status: http.StatusUnauthorized, Code: "invalid_state", Detail: "the sign-in expired or was started in another browser; start again"},
		{Err: authdomain.ErrSocialEmailUnverified, Status: http.StatusForbidden, Code: "social_email_unverified", Detail: "the provider hasn't verified this email address"},
		{Err: authdomain.ErrInvalidReturnTo, Status: http.StatusUnprocessableEntity, Code: "invalid_return_to", Detail: "return_to must be an absolute URL on the API's origin or APP_CORS_ORIGINS"},
		{Err: authdomain.ErrIdentityNotFound, Status: http.StatusNotFound, Code: "identity_not_found", Detail: "no linked account of yours has this ID"},
		{Err: authdomain.ErrLastSignInMethod, Status: http.StatusConflict, Code: "last_sign_in_method", Detail: "this is the account's last way to sign in; set a password or add a passkey first"},
		{Err: authdomain.ErrSocialUnavailable, Status: http.StatusServiceUnavailable, Code: "social_unavailable", Detail: "this sign-in provider isn't configured on this server (see AUTH_PROVIDERS.md)"},
		{Err: authdomain.ErrSocialLinkRequired, Status: http.StatusForbidden, Code: "social_link_required", Detail: "an account with this email address exists; sign in to it and link this provider from the account"},
		{Err: authdomain.ErrIdentityInUse, Status: http.StatusConflict, Code: "identity_in_use", Detail: "this Google, Apple or GitHub account is linked to another account"},
		{Err: authlib.ErrHasherBusy, Status: http.StatusServiceUnavailable, Code: "auth_unavailable", Detail: "authentication is temporarily unavailable; try again shortly"},
		{Err: authdomain.ErrRegistrationClosed, Status: http.StatusForbidden, Code: "registration_closed", Detail: "this app doesn't create accounts by signing up; ask for an invitation, or sign in to an existing account"},

		// API keys and service accounts (ADR-0058).
		{Err: authdomain.ErrSessionRequired, Status: http.StatusForbidden, Code: "session_required", Detail: "sign in to do this: an API key can't manage accounts, sessions or API keys"},
		{Err: authdomain.ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Detail: "missing permission for this operation"},
		{Err: authdomain.ErrStepUpRequired, Status: http.StatusForbidden, Code: "mfa_required", Detail: "sign in with two-factor authentication to use this operation; turn it on first if needed"},
		{Err: authdomain.ErrAPIKeyNotFound, Status: http.StatusNotFound, Code: "api_key_not_found", Detail: "no API key here has this ID"},
		{Err: authdomain.ErrInvalidAPIKeyName, Status: http.StatusUnprocessableEntity, Code: "invalid_api_key_name", Detail: "an API key name must be 1 to 100 characters on one line"},
		{Err: authdomain.ErrInvalidAPIKeyExpiry, Status: http.StatusUnprocessableEntity, Code: "invalid_api_key_expiry", Detail: "expires_at must be at least an hour away and within auth.api_key_max_ttl"},
		{Err: authdomain.ErrInvalidAPIKeyScopes, Status: http.StatusUnprocessableEntity, Code: "invalid_api_key_scopes", Detail: "scopes must be at most 50 permissions the key's owner holds without two-factor authentication"},
		{Err: authdomain.ErrAPIKeyLimitReached, Status: http.StatusConflict, Code: "api_key_limit_reached", Detail: "at most 20 usable API keys each; revoke one first"},
		{Err: authdomain.ErrServiceAccountNotFound, Status: http.StatusNotFound, Code: "service_account_not_found", Detail: "no service account here has this ID"},
		{Err: authdomain.ErrInvalidServiceAccount, Status: http.StatusUnprocessableEntity, Code: "invalid_service_account", Detail: "a service account name must be 1 to 100 characters on one line, and its description at most 500"},
		{Err: authdomain.ErrInvalidServiceAccountRole, Status: http.StatusUnprocessableEntity, Code: "invalid_service_account_role", Detail: "service accounts can't hold roles that require two-factor authentication or an organisation's owner role, nor a role above your own"},
		{Err: authdomain.ErrServiceAccountLimitReached, Status: http.StatusConflict, Code: "service_account_limit_reached", Detail: "at most 100 service accounts; delete one first"},
		{Err: authdomain.ErrServiceAccountDisabled, Status: http.StatusConflict, Code: "service_account_disabled", Detail: "the service account is disabled; enable it first"},

		// Operators' account APIs (ADR-0070).
		{Err: authdomain.ErrAccountBanned, Status: http.StatusForbidden, Code: "account_banned", Detail: "the account is banned"},
		{Err: authdomain.ErrUserNotFound, Status: http.StatusNotFound, Code: "user_not_found", Detail: "no account has this ID"},
		{Err: authdomain.ErrImpersonationOff, Status: http.StatusForbidden, Code: "impersonation_off", Detail: "impersonation is available only in development, with the dev console on"},
		{Err: authdomain.ErrInvalidCursor, Status: http.StatusBadRequest, Code: "invalid_cursor", Detail: "the cursor is not valid"},
		{Err: authdomain.ErrEmailTaken, Status: http.StatusConflict, Code: "email_taken", Detail: "an account already has this email address"},
		{Err: authdomain.ErrUnknownRole, Status: http.StatusUnprocessableEntity, Code: "unknown_role", Detail: "no such role in the permission catalog"},
	}
}
