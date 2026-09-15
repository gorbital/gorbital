// Package usecase holds the auth module's application logic: registration,
// email verification, sign-in, sessions, password reset and change, account
// deletion and platform roles. Security-sensitive steps (hashing, tokens,
// codes) use the apistock auth module; everything else is here to read and
// change.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"apistock.dev/actor"
	"apistock.dev/audit"
	"apistock.dev/config"
	authlib "apistock.dev/modules/auth"
	"apistock.dev/modules/auth/passkey"
	"apistock.dev/modules/auth/social"
	"apistock.dev/ratelimit"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
)

// Config holds the Service's dependencies and tunables.
type Config struct {
	// Required.
	Store    Store
	Catalog  *authlib.Catalog
	Recorder audit.Recorder
	Emails   authlib.Emails

	// Optional.
	Logger          *slog.Logger
	PasswordChecker authlib.PasswordChecker
	// Keyring encrypts authenticator app secrets (AUTH_ENCRYPTION_KEYS).
	// Without it, authenticator apps are unavailable.
	Keyring *authlib.Keyring
	// Passkeys runs passkey ceremonies (WEBAUTHN_RP_ID). Without it,
	// passkeys are unavailable.
	Passkeys *passkey.Service
	// Google and Apple sign people in with those providers (ADR-0046).
	// Without one, its endpoints answer ErrSocialUnavailable.
	Google *social.Provider
	Apple  *social.Provider
	// PublicURL is the API's public base URL (APP_PUBLIC_URL), which
	// providers return to at /v1/auth/{provider}/callback.
	PublicURL string
	// ReturnOrigins are the origins a web sign-in may return to, such as the
	// frontend's; DefaultReturnTo is used when a sign-in names none.
	ReturnOrigins   []string
	DefaultReturnTo string
	// Issuer names the app in authenticator apps. Default: "app".
	Issuer string
	// Hooks let other modules take part in creating and deleting accounts.
	Hooks AccountHooks
	// Durations, usually runtime settings. Each is clamped to the auth
	// module's hard limits.
	SessionIdleTTL          config.Value[time.Duration]
	SessionAbsoluteTTL      config.Value[time.Duration]
	VerificationCodeTTL     config.Value[time.Duration]
	ResetCodeTTL            config.Value[time.Duration]
	DeletedAccountRetention config.Value[time.Duration]
	// LoginLimiter limits sign-in attempts per address (second factors
	// included), MFALimiter changes to two-factor authentication per user,
	// and NoticeLimiter "account exists" emails per address. The app passes
	// limiters shared across instances (ADR-0052). Without them, in-memory
	// limiters allow LoginAttempts per LoginWindow and one notice a minute,
	// per instance.
	LoginLimiter  ratelimit.Taker
	MFALimiter    ratelimit.Taker
	NoticeLimiter ratelimit.Taker
	LoginAttempts int
	LoginWindow   time.Duration
	// Now is the clock, for tests.
	Now func() time.Time
}

// Service runs the auth use cases. It is safe for concurrent use.
type Service struct {
	store    Store
	catalog  *authlib.Catalog
	recorder audit.Recorder
	emails   authlib.Emails
	logger   *slog.Logger
	checker  authlib.PasswordChecker
	keyring  *authlib.Keyring
	passkeys *passkey.Service
	// providers are the configured sign-in providers by name.
	providers       map[string]*social.Provider
	publicURL       string
	returnOrigins   []string
	defaultReturnTo string
	issuer          string
	hooks           AccountHooks
	now             func() time.Time
	hasher          *authlib.Hasher
	loginLimiter    ratelimit.Taker
	mfaLimiter      ratelimit.Taker
	// noticeLimiter limits "account exists" emails per address, separately
	// from logins, so registrations can't lock the owner out.
	noticeLimiter ratelimit.Taker

	sessionIdle      config.Value[time.Duration]
	sessionAbsolute  config.Value[time.Duration]
	verificationCode config.Value[time.Duration]
	resetCode        config.Value[time.Duration]
	retention        config.Value[time.Duration]
}

// NewService returns a Service. It freezes the catalog.
func NewService(c Config) (*Service, error) {
	s := &Service{
		store:            c.Store,
		catalog:          c.Catalog,
		recorder:         c.Recorder,
		emails:           c.Emails,
		logger:           orDefault(c.Logger, slog.New(slog.DiscardHandler)),
		checker:          c.PasswordChecker,
		keyring:          c.Keyring,
		passkeys:         c.Passkeys,
		providers:        map[string]*social.Provider{},
		publicURL:        strings.TrimRight(c.PublicURL, "/"),
		defaultReturnTo:  c.DefaultReturnTo,
		issuer:           orDefault(c.Issuer, "app"),
		hooks:            c.Hooks,
		now:              c.Now,
		sessionIdle:      orDefault(c.SessionIdleTTL, config.Static(authlib.DefaultSessionIdleTTL)),
		sessionAbsolute:  orDefault(c.SessionAbsoluteTTL, config.Static(authlib.DefaultSessionAbsoluteTTL)),
		verificationCode: orDefault(c.VerificationCodeTTL, config.Static(authlib.DefaultVerificationCodeTTL)),
		resetCode:        orDefault(c.ResetCodeTTL, config.Static(authlib.DefaultResetCodeTTL)),
		retention:        orDefault(c.DeletedAccountRetention, config.Static(authlib.DefaultDeletedAccountRetention)),
	}
	if s.now == nil {
		s.now = time.Now
	}
	for _, p := range []*social.Provider{c.Google, c.Apple} {
		if p != nil {
			s.providers[p.Name()] = p
		}
	}
	for _, origin := range c.ReturnOrigins {
		s.returnOrigins = append(s.returnOrigins, strings.ToLower(strings.TrimRight(origin, "/")))
	}
	attempts, window := c.LoginAttempts, c.LoginWindow
	if attempts == 0 && window == 0 {
		attempts, window = authlib.DefaultLoginAttempts, authlib.DefaultLoginWindow
	}
	var errs []error
	if c.Store == nil || c.Catalog == nil || c.Recorder == nil || c.Emails == nil {
		errs = append(errs, errors.New("store, catalog, audit recorder and emails are required"))
	}
	if attempts < 1 || window <= 0 {
		errs = append(errs, errors.New("login limit needs at least 1 attempt in a positive window"))
	}
	if len(s.providers) > 0 && (s.publicURL == "" || s.defaultReturnTo == "") {
		errs = append(errs, errors.New("sign-in with Google or Apple needs the public URL and a default return address"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("auth: invalid service: %w", err)
	}
	hasher, err := authlib.NewHasher()
	if err != nil {
		return nil, err
	}
	c.Catalog.Freeze()
	s.hasher = hasher
	s.loginLimiter, s.mfaLimiter, s.noticeLimiter = c.LoginLimiter, c.MFALimiter, c.NoticeLimiter
	if s.loginLimiter == nil {
		s.loginLimiter = ratelimit.New(float64(attempts)/window.Seconds(), attempts, ratelimit.WithClock(s.now))
	}
	if s.mfaLimiter == nil {
		s.mfaLimiter = ratelimit.New(float64(attempts)/window.Seconds(), attempts, ratelimit.WithClock(s.now))
	}
	if s.noticeLimiter == nil {
		s.noticeLimiter = ratelimit.New(1/authlib.CodeResendInterval.Seconds(), 1, ratelimit.WithClock(s.now))
	}
	return s, nil
}

// allow asks limiter whether a request for key may proceed. A limiter that
// can't decide allows it: shared limiters fall back to memory themselves,
// so this only happens for a misconfigured limit.
func (s *Service) allow(ctx context.Context, limiter ratelimit.Taker, key string) (bool, time.Duration) {
	d, err := limiter.Take(ctx, key)
	if err != nil {
		s.logger.ErrorContext(ctx, "rate limiter couldn't decide; allowing the request", "err", err)
		return true, 0
	}
	return d.Allowed, d.RetryAfter
}

func orDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// Catalog returns the permission catalog.
func (s *Service) Catalog() *authlib.Catalog { return s.catalog }

// audit records e after the change it describes; a failed audit write is
// logged, not returned.
func (s *Service) audit(ctx context.Context, e audit.Event) {
	if e.Outcome == "" {
		e.Outcome = audit.OutcomeSuccess
	}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record auth audit event", "action", e.Action, "err", err)
	}
}

// userEvent is an event about userID. Actor fields come from the context
// unless set.
func userEvent(action, userID string, client authlib.ClientInfo) audit.Event {
	resourceType := "user"
	if userID == "" {
		resourceType = ""
	}
	return audit.Event{Action: action, ResourceType: resourceType, ResourceID: userID, IP: client.IP, UserAgent: client.UserAgent}
}

// sent logs an email of kind (such as verification_code) that couldn't be
// queued; the address is never logged. The operation has already succeeded;
// the user can ask for the email again.
func (s *Service) sent(ctx context.Context, kind string, err error) {
	if err != nil {
		s.logger.ErrorContext(ctx, "send auth email", "email_kind", kind, "err", err)
	}
}

// dbError hides driver errors, which aren't API (ADR-0018).
func dbError(op string, err error) error {
	return fmt.Errorf("auth: %s: %v", op, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
}

func requireActor(ctx context.Context) (actor.Actor, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind == actor.KindAnonymous || a.ID == "" {
		return actor.Actor{}, authdomain.ErrActorRequired
	}
	return a, nil
}

func requirePrincipal(ctx context.Context) (authlib.Principal, error) {
	p, ok := authlib.PrincipalFrom(ctx)
	if !ok {
		return authlib.Principal{}, authlib.ErrUnauthenticated
	}
	return p, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
