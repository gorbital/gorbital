package usecase

import (
	"context"
	"errors"
	"time"

	"gorbital.dev/audit"
	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

// auditEvent shortens audit.Event in this package.
type auditEvent = audit.Event

// RequestPasswordReset emails a reset code when the address has an account,
// at most once a minute. It returns nil either way, and takes as long, so it
// never reveals which addresses have accounts.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return err
	}
	defer s.padResponse(ctx, time.Now())
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
// reset code, and ends every session. For an unverified account, it verifies
// the address (the code proves ownership) and removes what was added before
// (claimAddress). The new password is hashed only once the code matched. It
// returns ErrInvalidCode, an *auth.PasswordError, or a *RateLimitError after
// auth.DefaultCodeAttempts checks for the address in a day.
func (s *Service) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	if err := authlib.ValidatePassword(ctx, newPassword, s.checker); err != nil {
		return err
	}
	_, normalized, err := authlib.NormalizeEmail(email)
	if err != nil {
		return authdomain.ErrInvalidCode
	}
	if err := s.allowCode(ctx, authdomain.PurposeResetPassword, normalized); err != nil {
		return err
	}
	var (
		userID, to string
		valid      bool
		revoked    int64
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
		hash, err := s.hasher.HashContext(ctx, newPassword)
		if err != nil {
			return err
		}
		now := s.now()
		userID, to = u.ID, u.Email
		if err := tx.UpdatePassword(ctx, u.ID, hash, now); err != nil {
			return err
		}
		// Whoever had the account's sessions or keys may be why the owner
		// resets the password.
		if revoked, err = tx.RevokeOwnerAPIKeys(ctx, u.ID, "", now, authdomain.RevokedPasswordReset); err != nil {
			return err
		}
		if _, err := tx.RevokeUserSessions(ctx, u.ID, "", now, "password_reset"); err != nil || u.EmailVerified() {
			return err
		}
		if err := tx.MarkEmailVerified(ctx, u.ID, now); err != nil {
			return err
		}
		return s.claimAddress(ctx, tx, u.ID, now, "password_reset")
	})
	if err != nil {
		return dbError("reset password", err)
	}
	if !valid {
		return authdomain.ErrInvalidCode
	}
	s.sent(ctx, "password_changed", s.emails.SendPasswordChanged(ctx, to))
	s.audit(ctx, userEvent("auth.password.reset", userID, authlib.ClientInfoFromContext(ctx)))
	s.keysRevoked(ctx, authdomain.OwnerUser, userID, "", revoked, authdomain.RevokedPasswordReset)
	return nil
}

// ChangePassword replaces the signed-in user's password after checking the
// current one, and ends the user's other sessions. It returns
// ErrInvalidCredentials for a wrong current password, an
// *auth.PasswordError, or a *RateLimitError when the user's
// re-authentication budget is spent.
func (s *Service) ChangePassword(ctx context.Context, currentPassword, newPassword string) error {
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return err
	}
	if err := authlib.ValidatePassword(ctx, newPassword, s.checker); err != nil {
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
		if valid, err = s.passwordMatches(ctx, u, currentPassword); err != nil || !valid {
			return err
		}
		hash, err := s.hasher.HashContext(ctx, newPassword)
		if err != nil {
			return err
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
		return s.reauthFailed(ctx, p.UserID, authdomain.ErrInvalidCredentials)
	}
	s.sent(ctx, "password_changed", s.emails.SendPasswordChanged(ctx, to))
	s.audit(ctx, userEvent("auth.password.changed", p.UserID, authlib.ClientInfoFromContext(ctx)))
	return nil
}
