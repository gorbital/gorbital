package usecase

import (
	"context"
	"errors"

	"apistock.dev/audit"
	authlib "apistock.dev/modules/auth"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
)

// auditEvent shortens audit.Event in this package.
type auditEvent = audit.Event

// RequestPasswordReset emails a reset code when the address has an account,
// at most once a minute. It returns nil either way, so it never reveals
// which addresses have accounts.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return err
	}
	ttl := authlib.ResetCodeLimits.Clamp(s.resetCode.Get(ctx))
	var userID, to, code string
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByEmail(ctx, normalized, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		userID, to = u.ID, u.Email
		code, err = s.issueCode(ctx, tx, u.ID, authdomain.PurposeResetPassword, ttl, authlib.CodeResendInterval)
		return err
	})
	switch {
	case errors.Is(err, errThrottled):
		return nil
	case err != nil:
		return dbError("request password reset", err)
	case code == "":
		return nil
	}
	s.sent(ctx, "password_reset_code", s.emails.SendPasswordResetCode(ctx, to, code, ttl))
	s.audit(ctx, userEvent("auth.password.reset_requested", userID, authlib.ClientInfoFromContext(ctx)))
	return nil
}

// ResetPassword sets a new password when code matches the account's newest
// reset code, verifies the address (the code proves ownership) and ends
// every session. It returns ErrInvalidCode or an *auth.PasswordError.
func (s *Service) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	if err := authlib.ValidatePassword(ctx, newPassword, s.checker); err != nil {
		return err
	}
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return authdomain.ErrInvalidCode
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	var (
		userID, to string
		valid      bool
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByEmail(ctx, normalized, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if valid, err = s.checkCode(ctx, tx, u.ID, authdomain.PurposeResetPassword, code); err != nil || !valid {
			return err // commit a failed attempt
		}
		now := s.now()
		userID, to = u.ID, u.Email
		if err := tx.UpdatePassword(ctx, u.ID, hash, now); err != nil {
			return err
		}
		if err := tx.MarkEmailVerified(ctx, u.ID, now); err != nil {
			return err
		}
		_, err = tx.RevokeUserSessions(ctx, u.ID, "", now, "password_reset")
		return err
	})
	if err != nil {
		return dbError("reset password", err)
	}
	if !valid {
		return authdomain.ErrInvalidCode
	}
	s.sent(ctx, "password_changed", s.emails.SendPasswordChanged(ctx, to))
	s.audit(ctx, userEvent("auth.password.reset", userID, authlib.ClientInfoFromContext(ctx)))
	return nil
}

// ChangePassword replaces the signed-in user's password after checking the
// current one, and ends the user's other sessions. It returns
// ErrInvalidCredentials for a wrong current password, or an
// *auth.PasswordError.
func (s *Service) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return err
	}
	if err := authlib.ValidatePassword(ctx, newPassword, s.checker); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	var to string
	valid := false
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			return nil
		}
		if err != nil || !u.HasPassword() {
			return err
		}
		if valid, _ = s.hasher.Verify(currentPassword, u.PasswordHash); !valid {
			return nil
		}
		now := s.now()
		to = u.Email
		if err := tx.UpdatePassword(ctx, u.ID, hash, now); err != nil {
			return err
		}
		if err := tx.ConsumeCodes(ctx, u.ID, authdomain.PurposeResetPassword, now); err != nil {
			return err
		}
		_, err = tx.RevokeUserSessions(ctx, u.ID, p.SessionID, now, "password_changed")
		return err
	})
	if err != nil {
		return dbError("change password", err)
	}
	if !valid {
		return authdomain.ErrInvalidCredentials
	}
	s.sent(ctx, "password_changed", s.emails.SendPasswordChanged(ctx, to))
	s.audit(ctx, userEvent("auth.password.changed", p.UserID, authlib.ClientInfoFromContext(ctx)))
	return nil
}
