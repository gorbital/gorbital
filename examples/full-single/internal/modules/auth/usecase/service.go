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
	"time"

	"apistock.dev/actor"
	"apistock.dev/audit"
	"apistock.dev/config"
	authlib "apistock.dev/modules/auth"
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
	// Without it, two-factor authentication is unavailable.
	Keyring *authlib.Keyring
	// Issuer names the app in authenticator apps. Default: "app".
	Issuer string
	// Durations, usually runtime settings. Each is clamped to the auth
	// module's hard limits.
	SessionIdleTTL          config.Value[time.Duration]
	SessionAbsoluteTTL      config.Value[time.Duration]
	VerificationCodeTTL     config.Value[time.Duration]
	ResetCodeTTL            config.Value[time.Duration]
	DeletedAccountRetention config.Value[time.Duration]
	// LoginAttempts per address within LoginWindow, per instance.
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
	issuer   string
	now      func() time.Time
	hasher   *authlib.Hasher
	limiter  *ratelimit.Limiter
	// notices limits "account exists" emails per address, separately from
	// logins, so registrations can't lock the owner out.
	notices *ratelimit.Limiter

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
		issuer:           orDefault(c.Issuer, "app"),
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
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("auth: invalid service: %w", err)
	}
	hasher, err := authlib.NewHasher()
	if err != nil {
		return nil, err
	}
	c.Catalog.Freeze()
	s.hasher = hasher
	s.limiter = ratelimit.New(float64(attempts)/window.Seconds(), attempts, ratelimit.WithClock(s.now))
	s.notices = ratelimit.New(1/authlib.CodeResendInterval.Seconds(), 1, ratelimit.WithClock(s.now))
	return s, nil
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
