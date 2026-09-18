package domain

import (
	"errors"
	"strings"
	"time"
)

// Second factors a sign-in accepts (ADR-0043).
const (
	MFAMethodTOTP         = "totp"
	MFAMethodRecoveryCode = "recovery_code"
)

// Errors returned by the two-factor authentication use cases.
var (
	// ErrInvalidMFA reports a wrong, used or expired second factor, or an
	// unknown, used, expired or exhausted sign-in challenge, without saying
	// which.
	ErrInvalidMFA = errors.New("invalid or expired second factor")
	// ErrMFAAlreadyEnabled reports two-factor authentication that is already
	// on.
	ErrMFAAlreadyEnabled = errors.New("two-factor authentication is already on")
	// ErrMFANotEnabled reports two-factor authentication that isn't on, or
	// whose setup wasn't started.
	ErrMFANotEnabled = errors.New("two-factor authentication is not on")
	// ErrMFARequiredByRole reports turning two-factor authentication off for
	// an account with a role that requires it.
	ErrMFARequiredByRole = errors.New("a role of this account requires two-factor authentication")
	// ErrMFAUnavailable reports a server without encryption keys
	// (AUTH_ENCRYPTION_KEYS), which can't use two-factor authentication.
	ErrMFAUnavailable = errors.New("two-factor authentication is not configured")
)

// TOTP is a user's authenticator app secret, stored encrypted.
type TOTP struct {
	UserID string
	// KeyID names the encryption key; SecretCiphertext never leaves the use
	// cases.
	KeyID            string
	SecretCiphertext []byte
	ConfirmedAt      *time.Time
	// LastUsedStep is the newest time step a code was used for.
	LastUsedStep *int64
	CreatedAt    time.Time
}

// Confirmed reports whether the user confirmed a code, turning two-factor
// authentication on.
func (t TOTP) Confirmed() bool { return t.ConfirmedAt != nil }

// MFAChallenge is a sign-in whose password matched, waiting for a second
// factor. Only its token's hash is stored.
type MFAChallenge struct {
	ID          string
	UserID      string
	TokenHash   []byte
	Attempts    int
	MaxAttempts int
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	IP          string
	UserAgent   string
	CreatedAt   time.Time
}

// UsableAt reports whether the challenge can still finish a sign-in at now.
func (c MFAChallenge) UsableAt(now time.Time) bool {
	return c.ConsumedAt == nil && now.Before(c.ExpiresAt) && c.Attempts < c.MaxAttempts
}

// SecondFactor is what a user gives as a second factor: a code from their
// authenticator app, a recovery code, or a passkey's response.
type SecondFactor struct {
	Code         string
	RecoveryCode string
	Passkey      *PasskeyAssertion
}

// Empty reports whether no second factor was given.
func (f SecondFactor) Empty() bool {
	return strings.TrimSpace(f.Code) == "" && strings.TrimSpace(f.RecoveryCode) == "" && f.Passkey == nil
}
