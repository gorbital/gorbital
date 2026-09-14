package usecase

import (
	"context"
	"errors"

	authlib "apistock.dev/modules/auth"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
)

// Register creates an account and emails a verification code. It returns
// only auth.ErrInvalidEmail or an *auth.PasswordError for bad input: when the
// address already has a verified account, the owner is emailed instead and
// the result is the same, so registration never reveals which addresses
// have accounts. Registering again before verifying replaces the password
// and sends a new code.
func (s *Service) Register(ctx context.Context, email, password string) error {
	email, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := authlib.ValidatePassword(ctx, password, s.checker); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return err
	}
	client := authlib.ClientInfoFromContext(ctx)
	ttl := authlib.VerificationCodeLimits.Clamp(s.verificationCode.Get(ctx))
	now := s.now()

	var (
		userID, to, code string
		exists           bool
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByEmail(ctx, normalized, true)
		switch {
		case errors.Is(err, authdomain.ErrUserNotFound):
			u, err = tx.InsertUser(ctx, authdomain.User{
				ID: authlib.NewID("usr"), Email: email, NormalizedEmail: normalized, PasswordHash: hash, CreatedAt: now,
			})
			if err != nil {
				return err
			}
		case err != nil:
			return err
		case u.EmailVerified():
			exists, to = true, u.Email
			return nil
		default:
			if err := tx.UpdatePassword(ctx, u.ID, hash, now); err != nil {
				return err
			}
		}
		userID, to = u.ID, u.Email
		code, err = s.issueCode(ctx, tx, userID, authdomain.PurposeVerifyEmail, ttl, 0)
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrEmailTaken):
		return nil // registered at the same moment by another request
	case err != nil:
		return dbError("register", err)
	case exists:
		s.sent(ctx, "account_exists", s.emails.SendAccountExists(ctx, to))
		return nil
	}
	s.sent(ctx, "verification_code", s.emails.SendVerificationCode(ctx, to, code, ttl))
	s.audit(ctx, userEvent("auth.user.registered", userID, client))
	return nil
}
