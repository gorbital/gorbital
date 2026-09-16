package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/ratelimitpg"
	"gorbital.dev/ratelimit"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// rateLimits are the request limits every instance shares through PostgreSQL
// (ADR-0052). Each reads its runtime settings on every request. When the
// database doesn't answer, they fall back to per-instance limits.
type rateLimits struct {
	store *ratelimitpg.Store
	// ip limits requests to /v1/auth/ per client IP address.
	ip ratelimit.Taker
	// login limits sign-in attempts per address, second factors included.
	login ratelimit.Taker
	// mfa limits changes to two-factor authentication per user.
	mfa ratelimit.Taker
	// notice limits "account exists" emails to one a minute per address.
	notice ratelimit.Taker
	// testEmail limits POST /ops/mail/test per operator.
	testEmail ratelimit.Taker
}

// newRateLimits builds the shared limiters on pool.
func newRateLimits(pool *pgxpool.Pool, s appSettings, logger *slog.Logger) (rateLimits, error) {
	store, err := ratelimitpg.NewStore(pool, ratelimitpg.WithLogger(logger))
	if err != nil {
		return rateLimits{}, err
	}
	limits := rateLimits{store: store}
	for _, l := range []struct {
		name  string
		dst   *ratelimit.Taker
		limit func(context.Context) ratelimit.Limit
	}{
		{"auth_ip", &limits.ip, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.authIPRequestsPerMinute.Get(ctx), time.Minute)
		}},
		{"auth_login", &limits.login, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.authLoginAttempts.Get(ctx), s.authLoginWindow.Get(ctx))
		}},
		{"auth_mfa", &limits.mfa, func(ctx context.Context) ratelimit.Limit {
			return ratelimit.Per(s.authMFAChangeAttempts.Get(ctx), s.authLoginWindow.Get(ctx))
		}},
		{"auth_notice", &limits.notice, func(context.Context) ratelimit.Limit {
			return ratelimit.Per(1, authlib.CodeResendInterval)
		}},
		{"ops_test_email", &limits.testEmail, func(context.Context) ratelimit.Limit {
			return ratelimit.Per(opsusecase.TestEmailsPerHour, time.Hour)
		}},
	} {
		limiter, err := store.Limiter(l.name, l.limit)
		if err != nil {
			return rateLimits{}, err
		}
		*l.dst = limiter
	}
	return limits, nil
}
