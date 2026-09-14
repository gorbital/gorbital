package usecase

import (
	"context"
	"errors"

	"apistock.dev/actor"
	"apistock.dev/audit"
	authlib "apistock.dev/modules/auth"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
)

// LoginResult is a new session.
type LoginResult struct {
	// Token authenticates later requests. It is shown once; only its hash is
	// stored.
	Token   string
	Session authdomain.Session
	User    authdomain.User
}

// Login checks the password and starts a new session. Unknown addresses and
// wrong passwords both return ErrInvalidCredentials after the same work. A
// correct password for an unverified address returns ErrEmailNotVerified.
// Too many attempts for one address return a *RateLimitError.
func (s *Service) Login(ctx context.Context, email, password string) (LoginResult, error) {
	client := authlib.ClientInfoFromContext(ctx)
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		s.hasher.VerifyDummy(password)
		return LoginResult{}, authdomain.ErrInvalidCredentials
	}
	if ok, retry := s.limiter.Allow(normalized); !ok {
		s.loginFailed(ctx, "", "rate_limited", client)
		return LoginResult{}, &authdomain.RateLimitError{RetryAfter: retry}
	}

	u, err := s.store.SelectUserByEmail(ctx, normalized, false)
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		s.hasher.VerifyDummy(password)
		s.loginFailed(ctx, "", "invalid_credentials", client)
		return LoginResult{}, authdomain.ErrInvalidCredentials
	case err != nil:
		return LoginResult{}, dbError("login", err)
	case !u.HasPassword():
		s.hasher.VerifyDummy(password)
		s.loginFailed(ctx, u.ID, "invalid_credentials", client)
		return LoginResult{}, authdomain.ErrInvalidCredentials
	}
	ok, rehash := s.hasher.Verify(password, u.PasswordHash)
	if !ok {
		s.loginFailed(ctx, u.ID, "invalid_credentials", client)
		return LoginResult{}, authdomain.ErrInvalidCredentials
	}
	if !u.EmailVerified() {
		s.loginFailed(ctx, u.ID, "email_not_verified", client)
		return LoginResult{}, authdomain.ErrEmailNotVerified
	}

	now := s.now()
	absolute := now.Add(authlib.SessionAbsoluteLimits.Clamp(s.sessionAbsolute.Get(ctx)))
	idle := minTime(now.Add(authlib.SessionIdleLimits.Clamp(s.sessionIdle.Get(ctx))), absolute)
	token, tokenHash := authlib.NewToken()
	var session authdomain.Session
	err = s.store.InTx(ctx, func(tx Store) error {
		var err error
		session, err = tx.InsertSession(ctx, authdomain.Session{
			ID: authlib.NewID("ses"), UserID: u.ID, TokenHash: tokenHash,
			CreatedAt: now, LastSeenAt: now, IdleExpiresAt: idle, AbsoluteExpiresAt: absolute,
			IP: client.IP, UserAgent: client.UserAgent,
		})
		if err != nil {
			return err
		}
		if rehash {
			if newHash, err := s.hasher.Hash(password); err == nil {
				if err := tx.RehashPassword(ctx, u.ID, newHash); err != nil {
					return err
				}
			}
		}
		u.Roles, err = tx.SelectUserRoles(ctx, u.ID)
		return err
	})
	if err != nil {
		return LoginResult{}, dbError("login", err)
	}

	e := userEvent("auth.login.succeeded", u.ID, client)
	e.ActorKind, e.ActorID = actor.KindUser, u.ID
	e.Metadata = map[string]any{"session_id": session.ID}
	s.audit(ctx, e)
	return LoginResult{Token: token, Session: session, User: u}, nil
}

func (s *Service) loginFailed(ctx context.Context, userID, reason string, client authlib.ClientInfo) {
	e := userEvent("auth.login.failed", userID, client)
	e.Outcome, e.Metadata = audit.OutcomeFailure, map[string]any{"reason": reason}
	s.audit(ctx, e)
}
