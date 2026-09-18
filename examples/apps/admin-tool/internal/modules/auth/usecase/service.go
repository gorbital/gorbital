// Package usecase holds the auth module's application logic: registration,
// email verification, sign-in, sessions, password reset and change, account
// deletion and platform roles. Security-sensitive steps (hashing, tokens,
// codes) use the gorbital auth module; everything else is here to read and
// change.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/auth/passkey"
	"gorbital.dev/modules/auth/social"
	"gorbital.dev/ratelimit"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

// RoleUser is the platform role every user holds without a grant
// (ADR-0058). The app's modules grant it the permissions
// any signed-in user has over their own data, such as their user-scoped
// resources, so an API key's scopes limit those operations too. It can't
// require two-factor authentication, and it is never granted or given to a
// service account. The name is public API.
const RoleUser = "user"

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
	// Google, Apple and GitHub sign people in with those providers
	// (ADR-0046, ADR-0059). Without one, its endpoints answer
	// ErrSocialUnavailable.
	Google *social.Provider
	Apple  *social.Provider
	GitHub *social.Provider
	// PublicURL is the API's public base URL (APP_PUBLIC_URL), which
	// providers return to at /v1/auth/{provider}/callback.
	PublicURL string
	// ReturnOrigins are the origins a web sign-in may return to, such as the
	// frontend's; DefaultReturnTo is used when a sign-in names none. Both
	// are required with a provider's web flow.
	ReturnOrigins   []string
	DefaultReturnTo string
	// Issuer names the app in authenticator apps. Default: "app".
	Issuer string
	// Hooks let other modules take part in creating and deleting accounts.
	Hooks AccountHooks
	// SignInHooks let the app take part in sign-in and account creation,
	// inside sign-in's transactions (authhttp's options).
	SignInHooks SignInHooks
	// RegistrationClosed refuses a first Google, Apple or GitHub sign-in of
	// an address without an account (ErrRegistrationClosed); authhttp also
	// leaves out POST /v1/auth/register.
	RegistrationClosed bool
	// Impersonation lets operators start a session as any user
	// (Impersonate); apps turn it on only with the dev console, so never in
	// production (ADR-0070).
	Impersonation bool
	// Orgs lets organisations manage their own service accounts in a
	// multi-tenant app (ADR-0058). Without it, only platform service
	// accounts exist.
	Orgs OrgAccess
	// APIKeyMaxTTL is the longest lifetime of a new API key, usually a
	// runtime setting, clamped to auth.APIKeyTTLLimits. Default:
	// auth.DefaultAPIKeyMaxTTL.
	APIKeyMaxTTL config.Value[time.Duration]
	// APIKeyLimiter limits failed API key authentications per client
	// network. The app passes a limiter shared across instances. Without
	// one, an in-memory limiter allows DefaultAPIKeyFailures a minute per
	// instance.
	APIKeyLimiter ratelimit.Taker
	// Durations, usually runtime settings. Each is clamped to the auth
	// module's hard limits.
	SessionIdleTTL          config.Value[time.Duration]
	SessionAbsoluteTTL      config.Value[time.Duration]
	VerificationCodeTTL     config.Value[time.Duration]
	ResetCodeTTL            config.Value[time.Duration]
	DeletedAccountRetention config.Value[time.Duration]
	UnverifiedAccountTTL    config.Value[time.Duration]
	// LoginLimiter limits sign-in attempts per address from one client
	// network, and LoginAddressLimiter per address from any network (second
	// factors included); MFALimiter limits changes to two-factor
	// authentication per user; ReauthLimiter password and second-factor
	// checks behind a session per user; CodeLimiter verification and reset
	// code checks per address; and NoticeLimiter "account exists" emails per
	// address. The app passes limiters shared across instances (ADR-0052).
	// Without them, in-memory limiters allow LoginAttempts per LoginWindow
	// (auth.DefaultLoginAddressAttempts per address), auth.DefaultCodeAttempts
	// code checks a day and one notice a minute, per instance.
	LoginLimiter        ratelimit.Taker
	LoginAddressLimiter ratelimit.Taker
	MFALimiter          ratelimit.Taker
	ReauthLimiter       ratelimit.Taker
	CodeLimiter         ratelimit.Taker
	NoticeLimiter       ratelimit.Taker
	LoginAttempts       int
	LoginWindow         time.Duration
	// MinResponseTime is the shortest time Register, ResendVerification and
	// RequestPasswordReset take, so their timing doesn't reveal whether an
	// address has an account. Default: auth.DefaultMinResponseTime; a
	// negative value turns it off, for tests.
	MinResponseTime time.Duration
	// HashConcurrency and HashMaxWait bound password hashing: how many
	// hashes run at once and how long a request waits for a turn before
	// failing with auth.ErrHasherBusy. Defaults: the auth module's.
	HashConcurrency int
	HashMaxWait     time.Duration
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
	signInHooks     SignInHooks
	closed          bool
	orgs            OrgAccess
	impersonation   bool
	now             func() time.Time
	hasher          *authlib.Hasher
	loginLimiter    ratelimit.Taker
	// loginAddressLimiter bounds sign-in attempts per address across
	// networks, looser than loginLimiter so nobody can cheaply lock the
	// owner out.
	loginAddressLimiter ratelimit.Taker
	mfaLimiter          ratelimit.Taker
	reauthLimiter       ratelimit.Taker
	codeLimiter         ratelimit.Taker
	// noticeLimiter limits "account exists" emails per address, separately
	// from logins, so registrations can't lock the owner out.
	noticeLimiter   ratelimit.Taker
	apiKeyLimiter   ratelimit.Taker
	minResponseTime time.Duration

	sessionIdle      config.Value[time.Duration]
	sessionAbsolute  config.Value[time.Duration]
	verificationCode config.Value[time.Duration]
	resetCode        config.Value[time.Duration]
	retention        config.Value[time.Duration]
	unverifiedTTL    config.Value[time.Duration]
	apiKeyMaxTTL     config.Value[time.Duration]
}

// NewService returns a Service. It freezes the catalog, which must declare
// RoleUser.
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
		signInHooks:      c.SignInHooks,
		closed:           c.RegistrationClosed,
		orgs:             c.Orgs,
		impersonation:    c.Impersonation,
		now:              c.Now,
		sessionIdle:      orDefault(c.SessionIdleTTL, config.Static(authlib.DefaultSessionIdleTTL)),
		sessionAbsolute:  orDefault(c.SessionAbsoluteTTL, config.Static(authlib.DefaultSessionAbsoluteTTL)),
		verificationCode: orDefault(c.VerificationCodeTTL, config.Static(authlib.DefaultVerificationCodeTTL)),
		resetCode:        orDefault(c.ResetCodeTTL, config.Static(authlib.DefaultResetCodeTTL)),
		retention:        orDefault(c.DeletedAccountRetention, config.Static(authlib.DefaultDeletedAccountRetention)),
		unverifiedTTL:    orDefault(c.UnverifiedAccountTTL, config.Static(authlib.DefaultUnverifiedAccountTTL)),
		apiKeyMaxTTL:     orDefault(c.APIKeyMaxTTL, config.Static(authlib.DefaultAPIKeyMaxTTL)),
		minResponseTime:  orDefault(c.MinResponseTime, authlib.DefaultMinResponseTime),
	}
	if s.now == nil {
		s.now = time.Now
	}
	for _, p := range []*social.Provider{c.Google, c.Apple, c.GitHub} {
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
	if c.Catalog != nil && (!c.Catalog.HasRole(RoleUser) || c.Catalog.RequiresMFA(RoleUser)) {
		errs = append(errs, fmt.Errorf("the catalog must declare the %s role, without two-factor authentication", RoleUser))
	}
	if attempts < 1 || window <= 0 {
		errs = append(errs, errors.New("login limit needs at least 1 attempt in a positive window"))
	}
	web := slices.ContainsFunc([]*social.Provider{c.Google, c.Apple, c.GitHub}, func(p *social.Provider) bool { return p != nil && p.Web() })
	if web && (s.publicURL == "" || s.defaultReturnTo == "") {
		errs = append(errs, errors.New("web sign-in with Google, Apple or GitHub needs the public URL and a default return address"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("auth: invalid service: %w", err)
	}
	hasher, err := authlib.NewHasherWith(authlib.WithHashConcurrency(c.HashConcurrency), authlib.WithHashMaxWait(c.HashMaxWait))
	if err != nil {
		return nil, err
	}
	c.Catalog.Freeze()
	s.hasher = hasher
	inMemory := func(taker ratelimit.Taker, n int, window time.Duration) ratelimit.Taker {
		if taker != nil {
			return taker
		}
		return ratelimit.New(float64(n)/window.Seconds(), n, ratelimit.WithClock(s.now))
	}
	s.loginLimiter = inMemory(c.LoginLimiter, attempts, window)
	s.loginAddressLimiter = inMemory(c.LoginAddressLimiter, max(attempts, authlib.DefaultLoginAddressAttempts), window)
	s.mfaLimiter = inMemory(c.MFALimiter, attempts, window)
	s.reauthLimiter = inMemory(c.ReauthLimiter, attempts, window)
	s.codeLimiter = inMemory(c.CodeLimiter, authlib.DefaultCodeAttempts, authlib.DefaultCodeWindow)
	s.noticeLimiter = inMemory(c.NoticeLimiter, 1, authlib.CodeResendInterval)
	s.apiKeyLimiter = inMemory(c.APIKeyLimiter, DefaultAPIKeyFailures, time.Minute)
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

// allowLogin charges a sign-in attempt for a normalized address: first
// from the client's network (an IPv4 address or IPv6 /64), then from any
// network. Keying the strict limit by network means someone else can't lock
// the owner out with a few wrong passwords from elsewhere (security review
// AUTH-S-6); the looser per-address limit still bounds guessing from many
// networks.
func (s *Service) allowLogin(ctx context.Context, normalized string, client authlib.ClientInfo) (bool, time.Duration) {
	if ok, retry := s.allow(ctx, s.loginLimiter, normalized+" "+ratelimit.ClientKey(client.IP)); !ok {
		return false, retry
	}
	return s.allow(ctx, s.loginAddressLimiter, normalized)
}

// allowCode charges one check of a verification or reset code for a
// normalized address, whether or not it has an account, across every code
// sent to it: a new code every minute would otherwise bring 5 new guesses
// (security review AUTH-S-2).
func (s *Service) allowCode(ctx context.Context, purpose, normalized string) error {
	if ok, retry := s.allow(ctx, s.codeLimiter, purpose+" "+normalized); !ok {
		return &authdomain.RateLimitError{RetryAfter: retry}
	}
	return nil
}

// padResponse waits until minResponseTime has passed since start (or ctx
// ends). Deferred by the anonymous flows whose work differs with whether the
// address has an account, it hides that difference (security review
// AUTH-S-4).
func (s *Service) padResponse(ctx context.Context, start time.Time) {
	wait := s.minResponseTime - time.Since(start)
	if wait <= 0 {
		return
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
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

// dbError hides driver errors, which aren't API (ADR-0018). A password
// hasher too busy to answer inside a transaction stays auth.ErrHasherBusy.
func dbError(op string, err error) error {
	if errors.Is(err, authlib.ErrHasherBusy) {
		return authlib.ErrHasherBusy
	}
	if errors.Is(err, authdomain.ErrAccountBanned) {
		return authdomain.ErrAccountBanned // every sign-in path starts a session (ADR-0070)
	}
	if r, ok := refusal(err); ok {
		return r // an app's hook refused, inside the transaction
	}
	return fmt.Errorf("auth: %s: %v", op, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
}

func requireActor(ctx context.Context) (actor.Actor, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind == actor.KindAnonymous || a.ID == "" {
		return actor.Actor{}, authdomain.ErrActorRequired
	}
	return a, nil
}

// requirePrincipal returns the request's signed-in session. A request
// authenticated with an API key gets ErrSessionRequired: keys can't change
// the account, its sign-in methods or sessions, or create keys (ADR-0058).
func requirePrincipal(ctx context.Context) (authlib.Principal, error) {
	p, ok := authlib.PrincipalFrom(ctx)
	switch {
	case !ok:
		return authlib.Principal{}, authlib.ErrUnauthenticated
	case p.APIKey() || p.UserID == "":
		return authlib.Principal{}, authdomain.ErrSessionRequired
	}
	return p, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
