package usecase

import (
	"context"
	"errors"
	"time"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// VerifyEmail marks the account's address as verified when code matches its
// newest verification code. Whatever was added to the account before its
// address was proven is removed (claimAddress). It returns ErrInvalidCode for
// a wrong, expired or used code, or an address without a pending code. Each
// code allows 5 attempts, and each address auth.DefaultCodeAttempts checks a
// day across codes (a *RateLimitError).
func (s *Service) VerifyEmail(ctx context.Context, email, code string) error {
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return authdomain.ErrInvalidCode
	}
	if err := s.allowCode(ctx, authdomain.PurposeVerifyEmail, normalized); err != nil {
		return err
	}
	var userID string
	valid := false
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByEmail(ctx, normalized, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return nil
		}
		if err != nil || u.EmailVerified() {
			return err
		}
		if valid, err = s.checkCode(ctx, tx, u.ID, authdomain.PurposeVerifyEmail, code); err != nil || !valid {
			return err // commit a failed attempt
		}
		userID = u.ID
		now := s.now()
		if err := tx.MarkEmailVerified(ctx, u.ID, now); err != nil {
			return err
		}
		return s.claimAddress(ctx, tx, u.ID, now, "email_verified")
	})
	if err != nil {
		return dbError("verify email", err)
	}
	if !valid {
		return authdomain.ErrInvalidCode
	}
	s.audit(ctx, userEvent("auth.email.verified", userID, authlib.ClientInfoFromContext(ctx)))
	return nil
}

// claimAddress is called in the transaction that first proves who owns an
// account's address. It ends the account's sessions and removes its
// passkeys, authenticator app, recovery codes and Google, Apple and GitHub
// identities (queuing Apple's tokens for revocation): whoever registered the
// address before it was proven keeps nothing (security review AUTH-S-1).
// Password sign-in needs a verified address, so such an account has these
// only from an identity whose provider isn't authoritative for the address
// (GitHub never is), which is removed too.
//
// A request signed in to the account itself removes nothing: whoever holds
// that session got it from the account's own identity, and now proved the
// address too, so both are the owner's.
func (s *Service) claimAddress(ctx context.Context, tx Store, userID string, now time.Time, reason string) error {
	if p, ok := authlib.PrincipalFrom(ctx); ok && !p.APIKey() && p.UserID == userID {
		return nil
	}
	if _, err := tx.RevokeUserSessions(ctx, userID, "", now, reason); err != nil {
		return err
	}
	if _, err := tx.RevokeOwnerAPIKeys(ctx, userID, "", now, authdomain.RevokedAddressClaimed); err != nil {
		return err
	}
	if _, err := tx.DeletePasskeys(ctx, userID); err != nil {
		return err
	}
	if _, err := tx.DeleteTOTP(ctx, userID); err != nil {
		return err
	}
	if err := tx.DeleteRecoveryCodes(ctx, userID); err != nil {
		return err
	}
	identities, err := tx.DeleteIdentities(ctx, userID)
	if err != nil {
		return err
	}
	return s.queueRevocations(ctx, tx, identities...)
}

// ResendVerification emails a new verification code to an unverified
// account, at most once a minute. It returns nil whether or not the address
// has an account, and takes as long either way.
func (s *Service) ResendVerification(ctx context.Context, email string) error {
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return err
	}
	defer s.padResponse(ctx, time.Now())
	ttl := authlib.VerificationCodeLimits.Clamp(s.verificationCode.Get(ctx))
	var to, code string
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByEmail(ctx, normalized, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return nil
		}
		if err != nil || u.EmailVerified() {
			return err
		}
		to = u.Email
		code, err = s.issueCode(ctx, tx, u.ID, authdomain.PurposeVerifyEmail, ttl, authlib.CodeResendInterval)
		return err
	})
	switch {
	case errors.Is(err, errThrottled):
		return nil
	case err != nil:
		return dbError("resend verification", err)
	case code != "":
		s.sent(ctx, "verification_code", s.emails.SendVerificationCode(ctx, to, code, ttl))
	}
	return nil
}
