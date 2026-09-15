package usecase

import (
	"context"
	"errors"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
)

// VerifyEmail marks the account's address as verified when code matches its
// newest verification code. It returns ErrInvalidCode for a wrong, expired
// or used code, or an address without a pending code. Each code allows 5
// attempts.
func (s *Service) VerifyEmail(ctx context.Context, email, code string) error {
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return authdomain.ErrInvalidCode
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
		return tx.MarkEmailVerified(ctx, u.ID, s.now())
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

// ResendVerification emails a new verification code to an unverified
// account, at most once a minute. It returns nil whether or not the address
// has an account.
func (s *Service) ResendVerification(ctx context.Context, email string) error {
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return err
	}
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
