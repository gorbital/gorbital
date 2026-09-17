package domain

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// MFAMethodPasskey is a passkey used as a second factor (ADR-0044).
const MFAMethodPasskey = "passkey"

// MaxPasskeys is how many passkeys one account can have.
const MaxPasskeys = 10

// Ceremony purposes.
const (
	CeremonyRegister     = "register"
	CeremonyLogin        = "login"
	CeremonySecondFactor = "second_factor"
	// CeremonyReauth confirms a signed-in user's sensitive change.
	CeremonyReauth = "reauth"
)

// Errors returned by the passkey use cases.
var (
	// ErrInvalidPasskey reports a passkey response that fails verification,
	// an unknown passkey, or an unknown, used or expired ceremony, without
	// saying which.
	ErrInvalidPasskey = errors.New("invalid passkey")
	// ErrPasskeyNotFound reports a passkey that doesn't exist or isn't the
	// user's.
	ErrPasskeyNotFound = errors.New("passkey not found")
	// ErrPasskeyLimitReached reports an account with MaxPasskeys passkeys.
	ErrPasskeyLimitReached = errors.New("too many passkeys")
	// ErrPasskeysUnavailable reports a server without a relying party
	// configuration (WEBAUTHN_RP_ID).
	ErrPasskeysUnavailable = errors.New("passkeys are not configured")
	// ErrInvalidPasskeyName reports a name that is empty or longer than 100
	// characters.
	ErrInvalidPasskeyName = errors.New("passkey name must be 1 to 100 characters")
)

// Passkey is a stored WebAuthn credential.
type Passkey struct {
	ID           string
	UserID       string
	CredentialID []byte
	// Record is the verified credential record, passed back unchanged to
	// the passkey library; it never leaves the use cases.
	Record         []byte
	Name           string
	AAGUID         []byte
	BackupEligible bool
	BackupState    bool
	SignCount      int64
	CreatedAt      time.Time
	LastUsedAt     *time.Time
}

// PasskeyName returns name trimmed, or "Passkey" when empty, or
// ErrInvalidPasskeyName when too long.
func PasskeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "Passkey", nil
	case utf8.RuneCountInString(name) > 100:
		return "", ErrInvalidPasskeyName
	}
	return name, nil
}

// WebAuthnCeremony is a started passkey ceremony, waiting for the client's
// response. Only its token's hash is stored.
type WebAuthnCeremony struct {
	ID        string
	TokenHash []byte
	// UserID is empty for a passwordless sign-in.
	UserID  string
	Purpose string
	// MFAChallengeID binds a second-factor ceremony to its sign-in.
	MFAChallengeID string
	SessionData    []byte
	ExpiresAt      time.Time
	ConsumedAt     *time.Time
	CreatedAt      time.Time
}

// UsableAt reports whether the ceremony can still be finished at now.
func (c WebAuthnCeremony) UsableAt(now time.Time) bool {
	return c.ConsumedAt == nil && now.Before(c.ExpiresAt)
}

// PasskeyAssertion is a passkey's response to a second-factor ceremony of a
// sign-in, or to a verification ceremony of a signed-in user.
type PasskeyAssertion struct {
	CeremonyToken string
	Credential    []byte
}
