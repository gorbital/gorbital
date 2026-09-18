package usecase

import (
	"context"
	"errors"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// LoginResult is a new session, or a sign-in waiting for a second factor.
type LoginResult struct {
	// Token authenticates later requests. It is shown once; only its hash is
	// stored. Empty when Challenge is set.
	Token   string
	Session authdomain.Session
	User    authdomain.User
	// Challenge is set instead of a session for an account with two-factor
	// authentication: finish signing in with LoginMFA.
	Challenge *MFAChallengeResult
}

// MFAChallengeResult is a sign-in waiting for its second factor.
type MFAChallengeResult struct {
	// Token identifies the sign-in in LoginMFA. It is shown once; only its
	// hash is stored.
	Token string
	// Methods are the second factors this server accepts for the account:
	// totp, passkey, recovery_code.
	Methods   []string
	ExpiresAt time.Time
}

// Login checks the password and starts a new session. Unknown addresses and
// wrong passwords both return ErrInvalidCredentials after the same work. A
// correct password for an unverified address returns ErrEmailNotVerified.
// Too many attempts for one address, from the client's network or from any,
// return a *RateLimitError, and a hasher too busy to check the password
// auth.ErrHasherBusy. For an account
// with two-factor authentication, it returns a challenge instead of a session
// (ADR-0043, ADR-0044), or ErrMFAUnavailable when this server can't check any
// of the account's factors.
func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	client := authlib.ClientInfoFromContext(ctx)
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		if err := s.hasher.VerifyDummyContext(ctx, password); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, authdomain.ErrInvalidCredentials
	}
	if ok, retry := s.allowLogin(ctx, normalized, client); !ok {
		s.loginFailed(ctx, "", "rate_limited", client)
		return LoginResult{}, &authdomain.RateLimitError{RetryAfter: retry}
	}

	u, err := s.store.SelectUserByEmail(ctx, normalized, false)
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		if err := s.hasher.VerifyDummyContext(ctx, password); err != nil {
			return LoginResult{}, err
		}
		s.loginFailed(ctx, "", "invalid_credentials", client)
		return LoginResult{}, authdomain.ErrInvalidCredentials
	case err != nil:
		return LoginResult{}, dbError("login", err)
	case !u.HasPassword():
		if err := s.hasher.VerifyDummyContext(ctx, password); err != nil {
			return LoginResult{}, err
		}
		s.loginFailed(ctx, u.ID, "invalid_credentials", client)
		return LoginResult{}, authdomain.ErrInvalidCredentials
	}
	ok, rehash, err := s.hasher.VerifyContext(ctx, password, u.PasswordHash)
	if err != nil {
		return LoginResult{}, err
	}
	if !ok {
		s.loginFailed(ctx, u.ID, "invalid_credentials", client)
		return LoginResult{}, authdomain.ErrInvalidCredentials
	}
	if !u.EmailVerified() {
		s.loginFailed(ctx, u.ID, "email_not_verified", client)
		return LoginResult{}, authdomain.ErrEmailNotVerified
	}

	methods, err := s.secondFactorMethods(ctx, u.ID)
	switch {
	case errors.Is(err, authdomain.ErrMFAUnavailable):
		s.logger.ErrorContext(ctx, "sign-in needs a second factor this server can't check: set AUTH_ENCRYPTION_KEYS or WEBAUTHN_RP_ID", "user_id", u.ID)
		return LoginResult{}, err
	case err != nil:
		return LoginResult{}, dbError("login", err)
	case len(methods) > 0:
		return s.startChallenge(ctx, u, password, rehash, client, methods, authdomain.MethodPassword)
	}

	var res LoginResult
	err = s.store.InTx(ctx, func(tx Store) error {
		if err := s.rehash(ctx, tx, u.ID, password, rehash); err != nil {
			return err
		}
		var err error
		res, err = s.startSignIn(ctx, tx, u, false, client, authdomain.MethodPassword, "")
		return err
	})
	if err != nil {
		return LoginResult{}, s.signInFailed(ctx, "login", u.ID, authdomain.MethodPassword, err, client)
	}
	s.loginSucceeded(ctx, res, authdomain.MethodPassword, "")
	s.afterLogin(ctx, res, authdomain.MethodPassword, "")
	return res, nil
}

// secondFactorMethods returns the second factors a sign-in to the account
// accepts on this server, or none when two-factor authentication is off. It
// returns ErrMFAUnavailable when it's on but this server can check neither
// the authenticator app (no encryption keys) nor passkeys (no relying party).
func (s *Service) secondFactorMethods(ctx context.Context, userID string) ([]string, error) {
	totp, found, err := s.store.SelectTOTP(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	passkeys, err := s.store.CountPasskeys(ctx, userID)
	if err != nil {
		return nil, err
	}
	hasTOTP := found && totp.Confirmed()
	if !hasTOTP && passkeys == 0 {
		return nil, nil
	}
	var methods []string
	if hasTOTP && s.keyring != nil {
		methods = append(methods, authdomain.MFAMethodTOTP)
	}
	if passkeys > 0 && s.passkeys != nil {
		methods = append(methods, authdomain.MFAMethodPasskey)
	}
	if len(methods) == 0 {
		return nil, authdomain.ErrMFAUnavailable
	}
	return append(methods, authdomain.MFAMethodRecoveryCode), nil
}

// startChallenge stores a sign-in challenge for a user whose first factor
// (method) is verified, to finish with LoginMFA. The challenge token names
// the method (domain.ChallengeToken).
func (s *Service) startChallenge(ctx context.Context, u authdomain.User, password string, rehash bool, client authlib.ClientInfo, methods []string, method string) (LoginResult, error) {
	now := s.now()
	raw, _ := authlib.NewToken()
	token := authdomain.ChallengeToken(raw, method)
	tokenHash := authlib.HashToken(token)
	c := authdomain.MFAChallenge{
		ID: authlib.NewID("mfc"), UserID: u.ID, TokenHash: tokenHash, MaxAttempts: authlib.MFAChallengeMaxAttempts,
		ExpiresAt: now.Add(authlib.MFAChallengeTTL), IP: client.IP, UserAgent: client.UserAgent, CreatedAt: now,
	}
	err := s.store.InTx(ctx, func(tx Store) error {
		if err := s.rehash(ctx, tx, u.ID, password, rehash); err != nil {
			return err
		}
		return tx.InsertMFAChallenge(ctx, c)
	})
	if err != nil {
		return LoginResult{}, dbError("login", err)
	}
	return LoginResult{User: u, Challenge: &MFAChallengeResult{Token: token, Methods: methods, ExpiresAt: c.ExpiresAt}}, nil
}

// rehash replaces a password hash made with older parameters. When hashing
// fails, the old hash stays and the next sign-in tries again.
func (s *Service) rehash(ctx context.Context, tx Store, userID, password string, needed bool) error {
	if !needed {
		return nil
	}
	if newHash, err := s.hasher.HashContext(ctx, password); err == nil {
		return tx.RehashPassword(ctx, userID, newHash)
	}
	return nil
}

// startSignIn creates a session for u in tx, verified with a second factor
// when mfaVerified, once u's factors are verified: a banned account is
// refused first, then the app's BeforeLogin hook runs. method and
// secondFactor say how u signed in.
func (s *Service) startSignIn(ctx context.Context, tx Store, u authdomain.User, mfaVerified bool, client authlib.ClientInfo, method, secondFactor string) (LoginResult, error) {
	if u.Banned() {
		return LoginResult{}, authdomain.ErrAccountBanned
	}
	if err := s.beforeLogin(ctx, tx, u, method, secondFactor, client); err != nil {
		return LoginResult{}, err
	}
	return s.startSession(ctx, tx, u, mfaVerified, client)
}

// signInFailed returns the error of a sign-in whose session couldn't be
// created, recording a hook's refusal.
func (s *Service) signInFailed(ctx context.Context, op, userID, method string, err error, client authlib.ClientInfo) error {
	if r, ok := refusal(err); ok {
		s.loginRefused(ctx, userID, method, r, client)
	}
	return dbError(op, err)
}

// startSession creates a session for u in tx, verified with a second factor
// when mfaVerified, and loads the user's roles.
func (s *Service) startSession(ctx context.Context, tx Store, u authdomain.User, mfaVerified bool, client authlib.ClientInfo) (LoginResult, error) {
	if u.Banned() {
		return LoginResult{}, authdomain.ErrAccountBanned
	}
	now := s.now()
	absolute := now.Add(authlib.SessionAbsoluteLimits.Clamp(s.sessionAbsolute.Get(ctx)))
	idle := minTime(now.Add(authlib.SessionIdleLimits.Clamp(s.sessionIdle.Get(ctx))), absolute)
	token, tokenHash := authlib.NewToken()
	in := authdomain.Session{
		ID: authlib.NewID("ses"), UserID: u.ID, TokenHash: tokenHash,
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: idle, AbsoluteExpiresAt: absolute,
		IP: client.IP, UserAgent: client.UserAgent,
	}
	if mfaVerified {
		in.MFAVerifiedAt = &now
	}
	session, err := tx.InsertSession(ctx, in)
	if err != nil {
		return LoginResult{}, err
	}
	if u.Roles, err = tx.SelectUserRoles(ctx, u.ID); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: token, Session: session, User: u}, nil
}

// loginSucceeded records a new session; mfaMethod names its second factor,
// or passkey for a passwordless sign-in. method, how the sign-in started, is
// recorded unless it is a password or a passkey, which v0.1's events imply.
func (s *Service) loginSucceeded(ctx context.Context, res LoginResult, method, mfaMethod string) {
	e := userEvent("auth.login.succeeded", res.User.ID, authlib.ClientInfoFromContext(ctx))
	e.ActorKind, e.ActorID = actor.KindUser, res.User.ID
	e.Metadata = map[string]any{"session_id": res.Session.ID}
	if mfaMethod != "" {
		e.Metadata["mfa_method"] = mfaMethod
	}
	if method != authdomain.MethodPassword && method != authdomain.MethodPasskey {
		e.Metadata["method"] = method
	}
	s.audit(ctx, e)
}

func (s *Service) loginFailed(ctx context.Context, userID, reason string, client authlib.ClientInfo) {
	e := userEvent("auth.login.failed", userID, client)
	e.Outcome, e.Metadata = audit.OutcomeFailure, map[string]any{"reason": reason}
	s.audit(ctx, e)
}
