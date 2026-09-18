package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorbital.dev/audit"
	authlib "gorbital.dev/modules/auth"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

// TOTPEnrollment is a new authenticator app secret, shown once.
type TOTPEnrollment struct {
	// Secret is typed into an authenticator app.
	Secret string
	// URI is the otpauth:// URI to show as a QR code.
	URI string
	// QRCode is a PNG image of URI as a data URL.
	QRCode string
}

// StartTOTPEnrollment creates a new authenticator app secret for the
// signed-in user after checking the password (for an account without one, a
// recent sign-in), replacing one that was never
// confirmed. Turn two-factor authentication on with ConfirmTOTP. It returns
// ErrInvalidCredentials, ErrMFAAlreadyEnabled, ErrMFAUnavailable or a
// *RateLimitError when the user's re-authentication budget is spent.
func (s *Service) StartTOTPEnrollment(ctx context.Context, password string) (TOTPEnrollment, error) {
	p, err := s.reauthPrincipal(ctx)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	if s.keyring == nil {
		return TOTPEnrollment{}, authdomain.ErrMFAUnavailable
	}
	secret := authlib.NewTOTPSecret()
	keyID, ciphertext, err := s.keyring.Encrypt([]byte(secret), totpAAD(p.UserID))
	if err != nil {
		return TOTPEnrollment{}, err
	}
	var (
		email string
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		if ok, err := s.passwordOrRecentSignIn(ctx, p, u, password); err != nil || !ok {
			state = authdomain.ErrInvalidCredentials
			return err
		}
		email = u.Email
		stored, err := tx.UpsertPendingTOTP(ctx, authdomain.TOTP{UserID: u.ID, KeyID: keyID, SecretCiphertext: ciphertext, CreatedAt: s.now()})
		if err == nil && !stored {
			state = authdomain.ErrMFAAlreadyEnabled
		}
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return TOTPEnrollment{}, authlib.ErrUnauthenticated
	case err != nil:
		return TOTPEnrollment{}, dbError("start two-factor setup", err)
	case state != nil:
		return TOTPEnrollment{}, s.reauthFailed(ctx, p.UserID, state)
	}
	uri := authlib.TOTPURI(s.issuer, email, secret)
	qr, err := authlib.TOTPQRCode(uri)
	if err != nil {
		return TOTPEnrollment{}, err
	}
	s.audit(ctx, userEvent("auth.mfa.totp_enrollment_started", p.UserID, authlib.ClientInfoFromContext(ctx)))
	return TOTPEnrollment{Secret: secret, URI: uri, QRCode: qr}, nil
}

// ConfirmTOTP turns the authenticator app on with a code from the app set up
// by StartTOTPEnrollment. It marks the current session verified with a second
// factor, ends the user's other sessions, and returns 10 recovery codes to
// show once (replacing earlier ones). It returns ErrInvalidMFA,
// ErrMFANotEnabled (setup wasn't started), ErrMFAAlreadyEnabled,
// ErrMFAUnavailable or a *RateLimitError.
func (s *Service) ConfirmTOTP(ctx context.Context, code string) ([]string, error) {
	p, err := s.mfaPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.keyring == nil {
		return nil, authdomain.ErrMFAUnavailable
	}
	codes := authlib.NewRecoveryCodes()
	var (
		to    string
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		totp, found, err := tx.SelectTOTP(ctx, u.ID, true)
		switch {
		case err != nil:
			return err
		case !found:
			state = authdomain.ErrMFANotEnabled
			return nil
		case totp.Confirmed():
			state = authdomain.ErrMFAAlreadyEnabled
			return nil
		}
		secret, err := s.decryptTOTP(totp)
		if err != nil {
			return err
		}
		step, match := authlib.VerifyTOTP(secret, code, s.now())
		if !match {
			state = authdomain.ErrInvalidMFA
			return nil
		}
		now := s.now()
		to = u.Email
		if err := tx.ConfirmTOTP(ctx, u.ID, step, now); err != nil {
			return err
		}
		if err := s.replaceRecoveryCodes(ctx, tx, u.ID, codes, now); err != nil {
			return err
		}
		if err := tx.MarkSessionMFAVerified(ctx, p.SessionID, now); err != nil {
			return err
		}
		_, err = tx.RevokeUserSessions(ctx, u.ID, p.SessionID, now, "mfa_enabled")
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return nil, authlib.ErrUnauthenticated
	case err != nil:
		return nil, dbError("turn on two-factor authentication", err)
	case state != nil:
		return nil, state
	}
	s.sent(ctx, "mfa_enabled", s.emails.SendTwoFactorEnabled(ctx, to))
	s.audit(ctx, userEvent("auth.mfa.totp_enabled", p.UserID, authlib.ClientInfoFromContext(ctx)))
	return codes, nil
}

// DisableTOTP turns the authenticator app off after checking the password and
// a second factor (a code, a recovery code, or a passkey's response to
// BeginPasskeyVerification), and ends the user's other sessions. Without passkeys left,
// it also deletes the recovery codes, and it refuses while a role requires
// two-factor authentication. It returns ErrInvalidCredentials, ErrInvalidMFA,
// ErrMFANotEnabled, ErrMFARequiredByRole or a *RateLimitError.
func (s *Service) DisableTOTP(ctx context.Context, password string, factor authdomain.SecondFactor) error {
	p, err := s.mfaPrincipal(ctx)
	if err != nil {
		return err
	}
	if _, err := s.reauthPrincipal(ctx); err != nil {
		return err
	}
	var (
		to    string
		state error
	)
	err = s.store.InTx(ctx, func(tx Store) error {
		u, err := tx.SelectUserByID(ctx, p.UserID, true)
		if err != nil {
			return err // ErrUserNotFound is handled below
		}
		if ok, err := s.passwordOrRecentSignIn(ctx, p, u, password); err != nil || !ok {
			state = authdomain.ErrInvalidCredentials
			return err
		}
		roles, err := tx.SelectUserRoles(ctx, u.ID)
		if err != nil {
			return err
		}
		passkeys, err := tx.CountPasskeys(ctx, u.ID)
		if err != nil {
			return err
		}
		if passkeys == 0 && s.catalog.RequiresMFA(roles...) {
			state = authdomain.ErrMFARequiredByRole
			return nil
		}
		if totp, found, err := tx.SelectTOTP(ctx, u.ID, true); err != nil || !found || !totp.Confirmed() {
			state = authdomain.ErrMFANotEnabled
			return err
		}
		if state, err = s.requireSecondFactor(ctx, tx, u.ID, factor); err != nil || state != nil {
			return err
		}
		now := s.now()
		to = u.Email
		if _, err := tx.DeleteTOTP(ctx, u.ID); err != nil {
			return err
		}
		if passkeys == 0 {
			if err := tx.DeleteRecoveryCodes(ctx, u.ID); err != nil {
				return err
			}
		}
		_, err = tx.RevokeUserSessions(ctx, u.ID, p.SessionID, now, "mfa_disabled")
		return err
	})
	switch {
	case errors.Is(err, authdomain.ErrUserNotFound):
		return authlib.ErrUnauthenticated
	case err != nil:
		return dbError("turn off two-factor authentication", err)
	case state != nil:
		return s.reauthFailed(ctx, p.UserID, state)
	}
	s.sent(ctx, "mfa_disabled", s.emails.SendTwoFactorDisabled(ctx, to))
	s.audit(ctx, userEvent("auth.mfa.totp_disabled", p.UserID, authlib.ClientInfoFromContext(ctx)))
	return nil
}

// RegenerateRecoveryCodes replaces the signed-in user's recovery codes and
// returns the new codes to show once. It needs a code from the authenticator
// app or a passkey's response to BeginPasskeyVerification; a recovery code
// can't replace the recovery codes. It returns ErrInvalidMFA,
// ErrMFANotEnabled or a *RateLimitError.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, factor authdomain.SecondFactor) ([]string, error) {
	p, err := s.mfaPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	factor.RecoveryCode = "" // never used up here
	codes := authlib.NewRecoveryCodes()
	var state error
	err = s.store.InTx(ctx, func(tx Store) error {
		has, err := s.hasSecondFactor(ctx, tx, p.UserID)
		switch {
		case err != nil:
			return err
		case !has:
			state = authdomain.ErrMFANotEnabled
			return nil
		}
		if state, err = s.requireSecondFactor(ctx, tx, p.UserID, factor); err != nil || state != nil {
			return err
		}
		return s.replaceRecoveryCodes(ctx, tx, p.UserID, codes, s.now())
	})
	if err != nil {
		return nil, dbError("replace recovery codes", err)
	}
	if state != nil {
		return nil, state
	}
	s.audit(ctx, userEvent("auth.mfa.recovery_codes_regenerated", p.UserID, authlib.ClientInfoFromContext(ctx)))
	return codes, nil
}

// mfaPrincipal returns the signed-in principal for a two-factor change,
// checking the user isn't guessing codes too fast.
func (s *Service) mfaPrincipal(ctx context.Context) (authlib.Principal, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return authlib.Principal{}, err
	}
	if ok, retry := s.allow(ctx, s.mfaLimiter, p.UserID); !ok {
		return authlib.Principal{}, &authdomain.RateLimitError{RetryAfter: retry}
	}
	return p, nil
}

// reauthPrincipal returns the signed-in principal for a change that checks
// the password or a second factor, spending one attempt of the user's
// re-authentication budget (auth.reauth_attempts): a stolen session must not
// become a way to guess the password or codes without the sign-in limit
// (security review AUTH-S-5, AUTH-M-2).
func (s *Service) reauthPrincipal(ctx context.Context) (authlib.Principal, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return authlib.Principal{}, err
	}
	if ok, retry := s.allow(ctx, s.reauthLimiter, p.UserID); !ok {
		s.reauthAudit(ctx, p.UserID, "rate_limited")
		return authlib.Principal{}, &authdomain.RateLimitError{RetryAfter: retry}
	}
	return p, nil
}

// reauthFailed records a wrong password or second factor given behind a
// session, and returns state.
func (s *Service) reauthFailed(ctx context.Context, userID string, state error) error {
	switch {
	case errors.Is(state, authdomain.ErrInvalidCredentials):
		s.reauthAudit(ctx, userID, "invalid_credentials")
	case errors.Is(state, authdomain.ErrInvalidMFA):
		s.reauthAudit(ctx, userID, "invalid_mfa")
	}
	return state
}

func (s *Service) reauthAudit(ctx context.Context, userID, reason string) {
	e := userEvent("auth.reauth.failed", userID, authlib.ClientInfoFromContext(ctx))
	e.Outcome, e.Metadata = audit.OutcomeFailure, map[string]any{"reason": reason}
	s.audit(ctx, e)
}

// passwordMatches reports whether password is u's. It returns
// auth.ErrHasherBusy when it couldn't check.
func (s *Service) passwordMatches(ctx context.Context, u authdomain.User, password string) (bool, error) {
	if !u.HasPassword() {
		return false, nil
	}
	ok, _, err := s.hasher.VerifyContext(ctx, password, u.PasswordHash)
	return ok, err
}

// passwordOrRecentSignIn confirms the account's owner before a sensitive
// change: the password, or for an account without one (Google, Apple or
// GitHub only), a session that started within auth.RecentVerification
// (ADR-0046).
func (s *Service) passwordOrRecentSignIn(ctx context.Context, p authlib.Principal, u authdomain.User, password string) (bool, error) {
	if u.HasPassword() {
		return s.passwordMatches(ctx, u, password)
	}
	return p.RecentlySignedIn(s.now()), nil
}

// hasSecondFactor reports whether the user has two-factor authentication on:
// a confirmed authenticator app or at least one passkey (ADR-0044).
func (s *Service) hasSecondFactor(ctx context.Context, store Store, userID string) (bool, error) {
	totp, found, err := store.SelectTOTP(ctx, userID, false)
	if err != nil {
		return false, err
	}
	if found && totp.Confirmed() {
		return true, nil
	}
	n, err := store.CountPasskeys(ctx, userID)
	return n > 0, err
}

// requireSecondFactor checks a code, recovery code or passkey response (to
// BeginPasskeyVerification) when the user has two-factor authentication on,
// returning ErrInvalidMFA as state when it fails and nil when it passes or
// isn't needed.
func (s *Service) requireSecondFactor(ctx context.Context, tx Store, userID string, factor authdomain.SecondFactor) (state, err error) {
	has, err := s.hasSecondFactor(ctx, tx, userID)
	if err != nil || !has {
		return nil, err
	}
	_, _, valid, err := s.checkSecondFactor(ctx, tx, userID, "", factor)
	if err != nil || valid {
		return nil, err
	}
	return authdomain.ErrInvalidMFA, nil
}

// checkSecondFactor checks a passkey's response (to the sign-in challenge
// challengeID, or with none to a verification ceremony), a code from the
// user's authenticator app, or a recovery code,
// and uses it up: a code's time step can't be used again, a recovery code is
// marked used, and a passkey ceremony can't be finished twice. It returns the
// method, the recovery codes left after using one, and whether the factor was
// valid.
func (s *Service) checkSecondFactor(ctx context.Context, tx Store, userID, challengeID string, factor authdomain.SecondFactor) (method string, remaining int, valid bool, err error) {
	switch {
	case factor.Passkey != nil:
		return s.checkPasskeyFactor(ctx, tx, userID, challengeID, *factor.Passkey)
	case strings.TrimSpace(factor.Code) != "":
		totp, found, err := tx.SelectTOTP(ctx, userID, true)
		if err != nil || !found || !totp.Confirmed() || s.keyring == nil {
			return authdomain.MFAMethodTOTP, 0, false, err
		}
		secret, err := s.decryptTOTP(totp)
		if err != nil {
			return authdomain.MFAMethodTOTP, 0, false, err
		}
		step, match := authlib.VerifyTOTP(secret, factor.Code, s.now())
		if !match {
			return authdomain.MFAMethodTOTP, 0, false, nil
		}
		used, err := tx.UseTOTPStep(ctx, userID, step)
		return authdomain.MFAMethodTOTP, 0, used, err
	}
	used, err := tx.UseRecoveryCode(ctx, userID, authlib.HashRecoveryCode(userID, factor.RecoveryCode), s.now())
	if err != nil || !used {
		return authdomain.MFAMethodRecoveryCode, 0, false, err
	}
	remaining, err = tx.CountUnusedRecoveryCodes(ctx, userID)
	return authdomain.MFAMethodRecoveryCode, remaining, true, err
}

func (s *Service) decryptTOTP(t authdomain.TOTP) (string, error) {
	if s.keyring == nil {
		return "", authdomain.ErrMFAUnavailable
	}
	plain, err := s.keyring.Decrypt(t.KeyID, t.SecretCiphertext, totpAAD(t.UserID))
	if err != nil {
		return "", fmt.Errorf("decrypt the TOTP secret with key %q: %w", t.KeyID, err)
	}
	return string(plain), nil
}

// replaceRecoveryCodes stores the hashes of a user's new recovery codes.
func (s *Service) replaceRecoveryCodes(ctx context.Context, tx Store, userID string, codes []string, now time.Time) error {
	ids, hashes := make([]string, len(codes)), make([][]byte, len(codes))
	for i, c := range codes {
		ids[i], hashes[i] = authlib.NewID("rec"), authlib.HashRecoveryCode(userID, c)
	}
	return tx.ReplaceRecoveryCodes(ctx, userID, ids, hashes, now)
}

// totpAAD binds an encrypted TOTP secret to its user, so a ciphertext copied
// to another user's row can't be decrypted.
func totpAAD(userID string) []byte { return []byte(userID + ":totp") }
