package usecase

import (
	"context"
	"errors"
	"fmt"

	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

// EnrollTOTP turns two-factor authentication on for a user on an operator's
// behalf, such as seed data's administrator: it stores a confirmed
// authenticator app secret and recovery codes, and returns them to show once.
// It returns ErrActorRequired, ErrUserNotFound, ErrMFAAlreadyEnabled or
// ErrMFAUnavailable.
func (s *Service) EnrollTOTP(ctx context.Context, userID string) (TOTPEnrollment, []string, error) {
	if _, err := requireActor(ctx); err != nil {
		return TOTPEnrollment{}, nil, err
	}
	if s.keyring == nil {
		return TOTPEnrollment{}, nil, authdomain.ErrMFAUnavailable
	}
	secret, codes := authlib.NewTOTPSecret(), authlib.NewRecoveryCodes()
	keyID, ciphertext, err := s.keyring.Encrypt([]byte(secret), totpAAD(userID))
	if err != nil {
		return TOTPEnrollment{}, nil, err
	}
	var (
		email string
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, userID, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			state = err
			return nil
		}
		if err != nil {
			return err
		}
		now := s.now()
		stored, err := tx.UpsertPendingTOTP(ctx, authdomain.TOTP{UserID: u.ID, KeyID: keyID, SecretCiphertext: ciphertext, CreatedAt: now})
		if err != nil || !stored {
			state = authdomain.ErrMFAAlreadyEnabled
			return err
		}
		email = u.Email
		// No code was used yet: any step is later than 0.
		if err := tx.ConfirmTOTP(ctx, u.ID, 0, now); err != nil {
			return err
		}
		return s.replaceRecoveryCodes(ctx, tx, u.ID, codes, now)
	})
	if err != nil {
		return TOTPEnrollment{}, nil, dbError("turn on two-factor authentication", err)
	}
	if state != nil {
		return TOTPEnrollment{}, nil, state
	}
	s.sent(ctx, "mfa_enabled", s.emails.SendTwoFactorEnabled(ctx, email))
	e := userEvent("auth.mfa.totp_enabled", userID, authlib.ClientInfo{})
	e.Metadata = map[string]any{"by_operator": true}
	s.audit(ctx, e)
	return TOTPEnrollment{Secret: secret, URI: authlib.TOTPURI(s.issuer, email, secret)}, codes, nil
}

// ResetMFA turns two-factor authentication off for a user who lost both
// their authenticator app and recovery codes, on an operator's behalf, and
// ends all their sessions. It returns ErrActorRequired, ErrUserNotFound or
// ErrMFANotEnabled.
func (s *Service) ResetMFA(ctx context.Context, userID string) error {
	if _, err := requireActor(ctx); err != nil {
		return err
	}
	var (
		email string
		state error
	)
	err := s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, userID, true)
		if errors.Is(err, authdomain.ErrUserNotFound) {
			state = err
			return nil
		}
		if err != nil {
			return err
		}
		deleted, err := tx.DeleteTOTP(ctx, u.ID)
		if err != nil {
			return err
		}
		passkeys, err := tx.DeletePasskeys(ctx, u.ID)
		if err != nil {
			return err
		}
		if !deleted && passkeys == 0 {
			state = authdomain.ErrMFANotEnabled
			return nil
		}
		email = u.Email
		if err := tx.DeleteRecoveryCodes(ctx, u.ID); err != nil {
			return err
		}
		_, err = tx.RevokeUserSessions(ctx, u.ID, "", s.now(), "mfa_reset")
		return err
	})
	if err != nil {
		return dbError("reset two-factor authentication", err)
	}
	if state != nil {
		return state
	}
	s.sent(ctx, "mfa_disabled", s.emails.SendTwoFactorDisabled(ctx, email))
	s.audit(ctx, userEvent("auth.mfa.reset", userID, authlib.ClientInfo{}))
	return nil
}

// RotateEncryptionKeys re-encrypts every authenticator app secret and Apple
// refresh token, stored or queued for revocation, with the current (first)
// key of AUTH_ENCRYPTION_KEYS and returns how many changed.
// Put a new key first, run it, then remove the old key. It returns
// ErrActorRequired or ErrMFAUnavailable.
func (s *Service) RotateEncryptionKeys(ctx context.Context) (int, error) {
	if _, err := requireActor(ctx); err != nil {
		return 0, err
	}
	if s.keyring == nil {
		return 0, authdomain.ErrMFAUnavailable
	}
	current, n := s.keyring.CurrentKeyID(), 0
	for {
		batch, err := s.store.SelectTOTPsWithOtherKey(ctx, current, 100)
		if err != nil {
			return n, dbError("rotate encryption keys", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, t := range batch {
			plain, err := s.keyring.Decrypt(t.KeyID, t.SecretCiphertext, totpAAD(t.UserID))
			if err != nil {
				return n, fmt.Errorf("auth: rotate encryption keys: the secret of %s: %w", t.UserID, err)
			}
			keyID, ciphertext, err := s.keyring.Encrypt(plain, totpAAD(t.UserID))
			if err != nil {
				return n, err
			}
			updated, err := s.store.UpdateTOTPSecret(ctx, t.UserID, t.KeyID, keyID, ciphertext)
			if err != nil {
				return n, dbError("rotate encryption keys", err)
			}
			if updated {
				n++
			}
		}
	}
	for {
		batch, err := s.store.SelectIdentitiesWithOtherKey(ctx, current, 100)
		if err != nil {
			return n, dbError("rotate encryption keys", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, i := range batch {
			plain, err := s.keyring.Decrypt(i.RefreshKeyID, i.RefreshTokenCiphertext, identityAAD(i.Provider, i.Subject))
			if err != nil {
				return n, fmt.Errorf("auth: rotate encryption keys: the refresh token of identity %s: %w", i.ID, err)
			}
			keyID, ciphertext, err := s.keyring.Encrypt(plain, identityAAD(i.Provider, i.Subject))
			if err != nil {
				return n, err
			}
			updated, err := s.store.UpdateIdentityRefreshKey(ctx, i.ID, i.RefreshKeyID, keyID, ciphertext)
			if err != nil {
				return n, dbError("rotate encryption keys", err)
			}
			if updated {
				n++
			}
		}
	}
	for {
		batch, err := s.store.SelectTokenRevocationsWithOtherKey(ctx, current, 100)
		if err != nil {
			return n, dbError("rotate encryption keys", err)
		}
		if len(batch) == 0 {
			break
		}
		for _, r := range batch {
			plain, err := s.keyring.Decrypt(r.KeyID, r.TokenCiphertext, identityAAD(r.Provider, r.Subject))
			if err != nil {
				return n, fmt.Errorf("auth: rotate encryption keys: the queued token %s: %w", r.ID, err)
			}
			keyID, ciphertext, err := s.keyring.Encrypt(plain, identityAAD(r.Provider, r.Subject))
			if err != nil {
				return n, err
			}
			updated, err := s.store.UpdateTokenRevocationKey(ctx, r.ID, r.KeyID, keyID, ciphertext)
			if err != nil {
				return n, dbError("rotate encryption keys", err)
			}
			if updated {
				n++
			}
		}
	}
	s.audit(ctx, auditEvent{Action: "auth.keys.rotated", Metadata: map[string]any{"key_id": current, "secrets": n}})
	return n, nil
}
