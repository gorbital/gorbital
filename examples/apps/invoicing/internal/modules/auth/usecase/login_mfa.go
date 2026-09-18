package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// LoginMFA finishes a sign-in that Login answered with a challenge, using a
// code from the authenticator app, an unused recovery code, or a passkey's
// response to BeginPasskeySecondFactor, and starts a session verified with a
// second factor. An unknown, expired, used or exhausted challenge and a wrong
// factor all return ErrInvalidMFA. Each challenge allows 5 attempts in 5
// minutes, and the attempts count toward the address's login limits
// (*RateLimitError).
func (s *Service) LoginMFA(ctx context.Context, challengeToken string, factor authdomain.SecondFactor) (LoginResult, error) {
	client := authlib.ClientInfoFromContext(ctx)
	switch {
	case challengeToken == "" || len(challengeToken) > 256 || factor.Empty():
		return LoginResult{}, authdomain.ErrInvalidMFA
	case factor.Passkey != nil && s.passkeys == nil:
		return LoginResult{}, authdomain.ErrPasskeysUnavailable
	case factor.Passkey == nil && strings.TrimSpace(factor.Code) != "" && s.keyring == nil:
		return LoginResult{}, authdomain.ErrMFAUnavailable
	}
	started := authdomain.ChallengeMethod(challengeToken)
	var (
		res            LoginResult
		userID, method string
		remaining      int
		valid, limited bool
		retry          time.Duration
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		c, found, err := tx.SelectMFAChallengeByTokenHash(ctx, authlib.HashToken(challengeToken))
		if err != nil || !found || !c.UsableAt(s.now()) {
			return err
		}
		userID = c.UserID
		u, err := tx.SelectUserByID(ctx, c.UserID, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if ok, wait := s.allowLogin(ctx, u.NormalizedEmail, client); !ok {
			limited, retry = true, wait
			return nil
		}
		if method, remaining, valid, err = s.checkSecondFactor(ctx, tx, u.ID, c.ID, factor); err != nil {
			return err
		}
		if !valid {
			return tx.FailMFAChallenge(ctx, c.ID, s.now()) // commit the failed attempt
		}
		if err := tx.ConsumeMFAChallenge(ctx, c.ID, s.now()); err != nil {
			return err
		}
		res, err = s.startSignIn(ctx, tx, u, true, client, started, method)
		return err
	})
	switch {
	case err != nil:
		return LoginResult{}, s.signInFailed(ctx, "finish sign-in", userID, started, err, client)
	case limited:
		s.mfaFailed(ctx, userID, "rate_limited", client)
		return LoginResult{}, &authdomain.RateLimitError{RetryAfter: retry}
	case !valid:
		s.mfaFailed(ctx, userID, "invalid_mfa", client)
		return LoginResult{}, authdomain.ErrInvalidMFA
	}

	e := userEvent("auth.mfa.challenge_succeeded", userID, client)
	e.ActorKind, e.ActorID = actor.KindUser, userID
	e.Metadata = map[string]any{"method": method}
	s.audit(ctx, e)
	if method == authdomain.MFAMethodRecoveryCode {
		s.recoveryCodeUsed(ctx, res.User, remaining)
	}
	s.loginSucceeded(ctx, res, started, method)
	s.afterLogin(ctx, res, started, method)
	return res, nil
}

func (s *Service) mfaFailed(ctx context.Context, userID, reason string, client authlib.ClientInfo) {
	e := userEvent("auth.mfa.challenge_failed", userID, client)
	e.Outcome, e.Metadata = audit.OutcomeFailure, map[string]any{"reason": reason}
	s.audit(ctx, e)
}

// recoveryCodeUsed tells the user a recovery code was used and records it.
func (s *Service) recoveryCodeUsed(ctx context.Context, u authdomain.User, remaining int) {
	s.sent(ctx, "recovery_code_used", s.emails.SendRecoveryCodeUsed(ctx, u.Email, remaining))
	e := userEvent("auth.mfa.recovery_code_used", u.ID, authlib.ClientInfoFromContext(ctx))
	e.Metadata = map[string]any{"remaining": remaining}
	s.audit(ctx, e)
}
