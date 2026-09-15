package usecase

import (
	"context"
	"time"

	authdomain "example.com/acme-api/internal/modules/auth/domain"
)

// The use cases own these ports; internal/modules/auth/repository
// implements them with SQL.

// UserStore reads and writes accounts. Deleted accounts are never returned.
type UserStore interface {
	// InsertUser creates an account, or returns ErrEmailTaken.
	InsertUser(ctx context.Context, u authdomain.User) (authdomain.User, error)
	// SelectUserByEmail returns the account for a normalized address, or
	// ErrUserNotFound. lock locks the row until the transaction ends.
	SelectUserByEmail(ctx context.Context, normalizedEmail string, lock bool) (authdomain.User, error)
	// SelectUserByID returns an account, or ErrUserNotFound.
	SelectUserByID(ctx context.Context, id string, lock bool) (authdomain.User, error)
	UpdatePassword(ctx context.Context, userID, passwordHash string, now time.Time) error
	// RehashPassword replaces the hash without changing password_changed_at.
	RehashPassword(ctx context.Context, userID, passwordHash string) error
	MarkEmailVerified(ctx context.Context, userID string, now time.Time) error
	MarkUserDeleted(ctx context.Context, userID string, now time.Time) error
	// DeleteDeletedUsers removes accounts deleted before before, with their
	// sessions, codes and roles.
	DeleteDeletedUsers(ctx context.Context, before time.Time) (int64, error)
}

// SessionStore reads and writes sessions.
type SessionStore interface {
	InsertSession(ctx context.Context, s authdomain.Session) (authdomain.Session, error)
	// SelectSessionByTokenHash returns a session and its account, or
	// ErrSessionNotFound.
	SelectSessionByTokenHash(ctx context.Context, tokenHash []byte) (authdomain.Session, authdomain.User, error)
	// SelectActiveSessions returns a user's sessions that haven't ended,
	// most recently used first.
	SelectActiveSessions(ctx context.Context, userID string, now time.Time) ([]authdomain.Session, error)
	TouchSession(ctx context.Context, id string, now, idleExpiresAt time.Time) error
	// RevokeSession ends one active session of userID and reports whether it
	// was active.
	RevokeSession(ctx context.Context, id, userID string, now time.Time, reason string) (bool, error)
	// RevokeUserSessions ends every active session of userID except keep
	// ("" keeps none).
	RevokeUserSessions(ctx context.Context, userID, keep string, now time.Time, reason string) (int64, error)
	DeleteEndedSessions(ctx context.Context, before time.Time) (int64, error)
}

// CodeStore reads and writes one-time codes.
type CodeStore interface {
	InsertCode(ctx context.Context, c authdomain.Code) error
	// ConsumeCodes ends every unused code of a purpose.
	ConsumeCodes(ctx context.Context, userID, purpose string, now time.Time) error
	SelectLatestCodeTime(ctx context.Context, userID, purpose string) (time.Time, bool, error)
	// SelectActiveCode locks the newest usable code of a purpose.
	SelectActiveCode(ctx context.Context, userID, purpose string, now time.Time) (authdomain.Code, bool, error)
	ConsumeCode(ctx context.Context, id string, now time.Time) error
	// FailCode counts a wrong guess and ends the code at its last attempt.
	FailCode(ctx context.Context, id string, now time.Time) error
	DeleteOldCodes(ctx context.Context, before time.Time) (int64, error)
}

// RoleStore reads and writes platform role assignments.
type RoleStore interface {
	// InsertUserRole grants a role and reports whether it was new.
	InsertUserRole(ctx context.Context, userID, role, grantedBy string, now time.Time) (bool, error)
	// DeleteUserRole removes a role and reports whether the user had it.
	DeleteUserRole(ctx context.Context, userID, role string) (bool, error)
	SelectUserRoles(ctx context.Context, userID string) ([]string, error)
}

// MFAStore reads and writes two-factor authentication: authenticator app
// secrets (encrypted), recovery codes (hashed), sign-in challenges and
// sessions verified with a second factor.
type MFAStore interface {
	// UpsertPendingTOTP stores an unconfirmed secret, replacing an
	// unconfirmed one, and reports false when the user's secret is already
	// confirmed.
	UpsertPendingTOTP(ctx context.Context, t authdomain.TOTP) (bool, error)
	// SelectTOTP returns a user's secret; lock locks it until the transaction
	// ends.
	SelectTOTP(ctx context.Context, userID string, lock bool) (authdomain.TOTP, bool, error)
	// ConfirmTOTP turns the secret on, recording step as used.
	ConfirmTOTP(ctx context.Context, userID string, step int64, now time.Time) error
	// UseTOTPStep records step as used when it is later than the last used
	// step, and reports whether it was.
	UseTOTPStep(ctx context.Context, userID string, step int64) (bool, error)
	// DeleteTOTP removes a user's secret and reports whether there was one.
	DeleteTOTP(ctx context.Context, userID string) (bool, error)
	// SelectTOTPsWithOtherKey returns up to limit secrets encrypted with a
	// key other than keyID.
	SelectTOTPsWithOtherKey(ctx context.Context, keyID string, limit int) ([]authdomain.TOTP, error)
	// UpdateTOTPSecret replaces a secret still encrypted with oldKeyID and
	// reports whether it did.
	UpdateTOTPSecret(ctx context.Context, userID, oldKeyID, keyID string, ciphertext []byte) (bool, error)
	// ReplaceRecoveryCodes deletes a user's recovery codes and stores new
	// hashes. Call it in a transaction.
	ReplaceRecoveryCodes(ctx context.Context, userID string, ids []string, hashes [][]byte, now time.Time) error
	// UseRecoveryCode marks an unused code used and reports whether it
	// matched one.
	UseRecoveryCode(ctx context.Context, userID string, hash []byte, now time.Time) (bool, error)
	CountUnusedRecoveryCodes(ctx context.Context, userID string) (int, error)
	DeleteRecoveryCodes(ctx context.Context, userID string) error
	InsertMFAChallenge(ctx context.Context, c authdomain.MFAChallenge) error
	// SelectMFAChallengeByTokenHash locks the challenge with a token's hash.
	SelectMFAChallengeByTokenHash(ctx context.Context, tokenHash []byte) (authdomain.MFAChallenge, bool, error)
	// FailMFAChallenge counts a wrong second factor and ends the challenge at
	// its last attempt.
	FailMFAChallenge(ctx context.Context, id string, now time.Time) error
	ConsumeMFAChallenge(ctx context.Context, id string, now time.Time) error
	DeleteOldMFAChallenges(ctx context.Context, before time.Time) (int64, error)
	MarkSessionMFAVerified(ctx context.Context, sessionID string, now time.Time) error
}

// Store is every storage operation the use cases need, plus transactions.
type Store interface {
	UserStore
	SessionStore
	CodeStore
	RoleStore
	MFAStore
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
