package authhttp

import (
	"context"
	"time"

	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/ratelimitpg"
	"gorbital.dev/ratelimit"
)

// rateLimits are sign-in's request limits, shared by every instance through
// PostgreSQL (ADR-0052), with v0.1's names. Each reads its runtime settings
// on every request; when the database doesn't answer, they fall back to
// per-instance limits. auth_ip, per client address on /v1/auth/, is the
// stack's RateLimit step.
type rateLimits struct {
	store *ratelimitpg.Store
	// login limits sign-in attempts per address from one client network,
	// and loginAddress per address from any network, second factors
	// included.
	login        ratelimit.Taker
	loginAddress ratelimit.Taker
	// mfa limits changes to two-factor authentication per user.
	mfa ratelimit.Taker
	// reauth limits password and second-factor checks behind a session per
	// user.
	reauth ratelimit.Taker
	// code limits verification and reset code checks per address.
	code ratelimit.Taker
	// notice limits "account exists" emails to one a minute per address.
	notice ratelimit.Taker
	// apiKey limits failed API key authentications per client network.
	apiKey ratelimit.Taker
}

// limiters are sign-in's limiters, in v0.1's order and words, as
// GET /ops/auth/rate-limits lists them (Module.RateLimiters), each with the
// sign-in method it belongs to. The names are public API: operators reset
// a key's budget by name.
var limiters = []struct {
	limiter gorbital.RateLimiter
	method  Method
}{
	{gorbital.RateLimiter{Name: "auth_login", Keys: "normalized email address and client network, joined with a space", Description: "Sign-in attempts per address from one network (auth.login_attempts)"}, MethodPassword},
	{gorbital.RateLimiter{Name: "auth_login_address", Keys: "normalized email address", Description: "Sign-in attempts per address across networks"}, MethodPassword},
	{gorbital.RateLimiter{Name: "auth_mfa", Keys: "user ID", Description: "Second-factor changes per user"}, MethodTOTP},
	{gorbital.RateLimiter{Name: "auth_reauth", Keys: "user ID", Description: "Password and second-factor checks behind a session per user"}, MethodPassword},
	{gorbital.RateLimiter{Name: "auth_code", Keys: "purpose and normalized email address, joined with a space", Description: "Verification and reset code checks per address (auth.code_attempts)"}, MethodPassword},
	{gorbital.RateLimiter{Name: "auth_notice", Keys: "normalized email address", Description: "\"Account exists\" emails per address"}, MethodPassword},
	{gorbital.RateLimiter{Name: "auth_api_key", Keys: "client network", Description: "Failed API key authentications per network"}, MethodAPIKeys},
}

// rateLimiters are the limiters of the methods the app serves: a method it
// doesn't serve has neither the limiter nor its budget in
// /ops/auth/rate-limits (ADR-0089).
func rateLimiters(o options) []gorbital.RateLimiter {
	out := make([]gorbital.RateLimiter, 0, len(limiters))
	for _, l := range limiters {
		if o.has(l.method) {
			out = append(out, l.limiter)
		}
	}
	return out
}

// newRateLimits builds the shared limiters of the methods the app serves.
// The use cases fall back to a per-instance limiter for the others, which
// no operation of theirs reaches.
func newRateLimits(store *ratelimitpg.Store, s *authSettings, o options) (*rateLimits, error) {
	limits := &rateLimits{store: store}
	for _, l := range []struct {
		name   string
		method Method
		dst    *ratelimit.Taker
		limit  func(context.Context) ratelimit.Limit
	}{
		{"auth_login", MethodPassword, &limits.login, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.loginAttempts.Get(ctx), s.loginWindow.Get(ctx))
		}},
		{"auth_login_address", MethodPassword, &limits.loginAddress, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.loginAddressAttempts.Get(ctx), s.loginWindow.Get(ctx))
		}},
		{"auth_mfa", MethodTOTP, &limits.mfa, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.mfaChangeAttempts.Get(ctx), s.loginWindow.Get(ctx))
		}},
		{"auth_reauth", MethodPassword, &limits.reauth, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.reauthAttempts.Get(ctx), s.loginWindow.Get(ctx))
		}},
		{"auth_code", MethodPassword, &limits.code, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.codeAttempts.Get(ctx), s.codeWindow.Get(ctx))
		}},
		{"auth_notice", MethodPassword, &limits.notice, func(context.Context) ratelimit.Limit {
			return ratelimit.Per(1, authlib.CodeResendInterval)
		}},
		{"auth_api_key", MethodAPIKeys, &limits.apiKey, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.apiKeyFailures.Get(ctx), time.Minute)
		}},
	} {
		if !o.has(l.method) {
			continue
		}
		taker, err := store.Limiter(l.name, l.limit)
		if err != nil {
			return nil, err
		}
		*l.dst = taker
	}
	return limits, nil
}
