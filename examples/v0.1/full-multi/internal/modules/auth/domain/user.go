package domain

import "time"

// User is an account.
type User struct {
	ID              string
	Email           string
	NormalizedEmail string
	// PasswordHash is the argon2id hash; empty for accounts without a
	// password. It never leaves the use cases.
	PasswordHash    string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	// WebAuthnUserHandle identifies the account in its passkeys; nil until
	// the first passkey is registered.
	WebAuthnUserHandle []byte
	// BannedAt is when an operator banned the account (ADR-0070); a banned
	// account can't sign in and its sessions and keys were revoked.
	BannedAt     *time.Time
	BannedReason string
	// Roles are the user's platform roles, when loaded.
	Roles []string
}

// EmailVerified reports whether the user proved they own the address.
func (u User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

// Banned reports whether an operator banned the account.
func (u User) Banned() bool { return u.BannedAt != nil }

// HasPassword reports whether the account can sign in with a password.
func (u User) HasPassword() bool { return u.PasswordHash != "" }

// Session is a signed-in device.
type Session struct {
	ID     string
	UserID string
	// TokenHash is the SHA-256 of the session token; the token itself is
	// never stored.
	TokenHash         []byte
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
	IP                string
	UserAgent         string
	// MFAVerifiedAt is when the session was verified with a second factor.
	MFAVerifiedAt *time.Time
}

// MFAVerified reports whether the session was verified with a second factor.
func (s Session) MFAVerified() bool { return s.MFAVerifiedAt != nil }

// ActiveAt reports whether the session can be used at now.
func (s Session) ActiveAt(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.IdleExpiresAt) && now.Before(s.AbsoluteExpiresAt)
}

// ExpiresAt is when the session ends unless used again.
func (s Session) ExpiresAt() time.Time {
	if s.IdleExpiresAt.Before(s.AbsoluteExpiresAt) {
		return s.IdleExpiresAt
	}
	return s.AbsoluteExpiresAt
}

// Code purposes.
const (
	PurposeVerifyEmail   = "verify_email"
	PurposeResetPassword = "reset_password"
)

// Code is a one-time code sent by email. Only its hash is stored.
type Code struct {
	ID          string
	UserID      string
	Purpose     string
	Hash        []byte
	Attempts    int
	MaxAttempts int
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

// CleanupResult counts what a cleanup removed.
type CleanupResult struct {
	Sessions   int64
	Codes      int64
	Challenges int64
	Users      int64
	// Unverified counts accounts deleted because their address was never
	// verified.
	Unverified int64
	// ExpiredAPIKeys counts keys whose expiry was recorded, and APIKeys the
	// expired or revoked keys removed.
	ExpiredAPIKeys int64
	APIKeys        int64
}
