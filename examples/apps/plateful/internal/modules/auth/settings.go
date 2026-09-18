package authhttp

import (
	"time"

	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/settings"

	"example.com/plateful/internal/modules/auth/usecase"
)

// authSettings are sign-in's runtime settings, with the keys, defaults and
// rules of a v0.1 app's settings.go, so values operators changed stay in
// effect (ADR-0031). Setting keys are public API. auth.ip_requests_per_minute
// is gorbital's: the stack's RateLimit step reads it.
type authSettings struct {
	sessionIdleTTL          *settings.Setting[time.Duration]
	sessionAbsoluteTTL      *settings.Setting[time.Duration]
	verificationCodeTTL     *settings.Setting[time.Duration]
	resetCodeTTL            *settings.Setting[time.Duration]
	deletedAccountRetention *settings.Setting[time.Duration]
	unverifiedAccountTTL    *settings.Setting[time.Duration]
	loginAttempts           *settings.Setting[int]
	loginAddressAttempts    *settings.Setting[int]
	loginWindow             *settings.Setting[time.Duration]
	mfaChangeAttempts       *settings.Setting[int]
	reauthAttempts          *settings.Setting[int]
	codeAttempts            *settings.Setting[int]
	codeWindow              *settings.Setting[time.Duration]
	apiKeyMaxTTL            *settings.Setting[time.Duration]
	apiKeyFailures          *settings.Setting[int]
}

// declared reports whether Module's Settings declared the settings.
func (s *authSettings) declared() bool { return s.sessionIdleTTL != nil }

// declareSettings declares sign-in's runtime settings. apiKeyCap is the
// longest auth.api_key_max_ttl operators may set (APIKeyMaxTTL; a year by
// default).
func declareSettings(reg *settings.Registry, apiKeyCap time.Duration) authSettings {
	if apiKeyCap == 0 {
		apiKeyCap = maxAPIKeyMaxTTL
	}
	return authSettings{
		sessionIdleTTL: settings.Duration(reg, "auth.session_idle_ttl", authlib.DefaultSessionIdleTTL,
			settings.Describe("How long a signed-in session lasts without being used."),
			settings.Range(time.Hour, 90*24*time.Hour),
			settings.ReasonRequired(),
		),
		sessionAbsoluteTTL: settings.Duration(reg, "auth.session_absolute_ttl", authlib.DefaultSessionAbsoluteTTL,
			settings.Describe("The longest a session lasts, however often it is used; then the user signs in again."),
			settings.Range(24*time.Hour, 365*24*time.Hour),
			settings.ReasonRequired(),
		),
		verificationCodeTTL: settings.Duration(reg, "auth.verification_code_ttl", authlib.DefaultVerificationCodeTTL,
			settings.Describe("How long email verification codes stay valid."),
			settings.Range(5*time.Minute, time.Hour),
			settings.ReasonRequired(),
		),
		resetCodeTTL: settings.Duration(reg, "auth.reset_code_ttl", authlib.DefaultResetCodeTTL,
			settings.Describe("How long password reset codes stay valid."),
			settings.Range(10*time.Minute, 2*time.Hour),
			settings.ReasonRequired(),
		),
		deletedAccountRetention: settings.Duration(reg, "auth.deleted_account_retention", authlib.DefaultDeletedAccountRetention,
			settings.Describe("How long deleted accounts are kept before the auth_cleanup job removes them."),
			settings.Range(24*time.Hour, 365*24*time.Hour),
			settings.ReasonRequired(),
		),
		unverifiedAccountTTL: settings.Duration(reg, "auth.unverified_account_ttl", authlib.DefaultUnverifiedAccountTTL,
			settings.Describe("How long an account whose email address was never verified stays before the auth_cleanup job deletes it, freeing the address. Accounts with a Google, Apple or GitHub sign-in are kept."),
			settings.Range(time.Hour, 90*24*time.Hour),
			settings.ReasonRequired(),
		),

		// Rate limits every instance shares (limits.go, ADR-0052).
		loginAttempts: settings.Int(reg, "auth.login_attempts", authlib.DefaultLoginAttempts,
			settings.Describe("Sign-in attempts allowed per email address from one client network (an IPv4 address or IPv6 /64) within auth.login_window, second factors included."),
			settings.Group("rate_limits"),
			settings.Range(3, 100),
			settings.ReasonRequired(),
		),
		loginAddressAttempts: settings.Int(reg, "auth.login_address_attempts", authlib.DefaultLoginAddressAttempts,
			settings.Describe("Sign-in attempts allowed per email address from all networks together within auth.login_window. Keep it well above auth.login_attempts: reaching it blocks the owner's sign-ins too."),
			settings.Group("rate_limits"),
			settings.Range(10, 1000),
			settings.ReasonRequired(),
		),
		loginWindow: settings.Duration(reg, "auth.login_window", authlib.DefaultLoginWindow,
			settings.Describe("The window of auth.login_attempts, auth.login_address_attempts, auth.mfa_change_attempts and auth.reauth_attempts."),
			settings.Group("rate_limits"),
			settings.Range(time.Minute, 24*time.Hour),
			settings.ReasonRequired(),
		),
		mfaChangeAttempts: settings.Int(reg, "auth.mfa_change_attempts", authlib.DefaultLoginAttempts,
			settings.Describe("Changes to two-factor authentication (confirming, turning off, replacing recovery codes) allowed per user within auth.login_window."),
			settings.Group("rate_limits"),
			settings.Range(3, 100),
			settings.ReasonRequired(),
		),
		reauthAttempts: settings.Int(reg, "auth.reauth_attempts", authlib.DefaultLoginAttempts,
			settings.Describe("Changes that check the password or a second factor of a signed-in user (changing the password, setting up or turning off two-factor authentication, adding or removing passkeys, linking or unlinking Google, Apple or GitHub, deleting the account) allowed per user within auth.login_window."),
			settings.Group("rate_limits"),
			settings.Range(3, 100),
			settings.ReasonRequired(),
		),
		codeAttempts: settings.Int(reg, "auth.code_attempts", authlib.DefaultCodeAttempts,
			settings.Describe("Email verification and password reset code checks allowed per email address within auth.code_window, across every code sent. Reaching it blocks the owner's codes too until the window passes."),
			settings.Group("rate_limits"),
			settings.Range(5, 100),
			settings.ReasonRequired(),
		),
		codeWindow: settings.Duration(reg, "auth.code_window", authlib.DefaultCodeWindow,
			settings.Describe("The window of auth.code_attempts."),
			settings.Group("rate_limits"),
			settings.Range(time.Hour, 7*24*time.Hour),
			settings.ReasonRequired(),
		),
		// API keys (ADR-0058).
		apiKeyMaxTTL: settings.Duration(reg, "auth.api_key_max_ttl", min(authlib.DefaultAPIKeyMaxTTL, apiKeyCap),
			settings.Describe("The longest lifetime of a new API key. Every key needs an expiry within it; existing keys keep theirs."),
			settings.Range(minAPIKeyMaxTTL, apiKeyCap),
			settings.ReasonRequired(),
		),
		apiKeyFailures: settings.Int(reg, "auth.api_key_failures_per_minute", usecase.DefaultAPIKeyFailures,
			settings.Describe("Requests with a malformed, unknown or wrong API key allowed per client network (an IPv4 address or IPv6 /64) per minute, across all instances; then 429. Valid keys aren't limited."),
			settings.Group("rate_limits"),
			settings.Range(5, 10_000),
			settings.ReasonRequired(),
		),
	}
}
