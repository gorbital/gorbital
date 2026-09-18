package usecase

import (
	"context"
	"time"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// The use cases own these ports; authhttp's internal/repository
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
	// RemovePassword removes the password, as when an unverified account is
	// registered again with another one.
	RemovePassword(ctx context.Context, userID string, now time.Time) error
	MarkEmailVerified(ctx context.Context, userID string, now time.Time) error
	MarkUserDeleted(ctx context.Context, userID string, now time.Time) error
	// SelectUsers lists accounts newest first for operators (ADR-0070):
	// matching query when set, after afterTime and afterID when afterTime
	// is set, at most limit.
	SelectUsers(ctx context.Context, query string, afterTime *time.Time, afterID string, limit int) ([]authdomain.User, error)
	// SetUserBan bans an account (bannedAt set) or lifts the ban (nil).
	SetUserBan(ctx context.Context, userID string, bannedAt *time.Time, reason string, now time.Time) error
	// MarkUnverifiedUsersDeleted soft-deletes up to limit accounts created
	// before createdBefore whose address was never verified, without a
	// Google, Apple or GitHub identity or a code sent since, and returns their IDs.
	MarkUnverifiedUsersDeleted(ctx context.Context, createdBefore, now time.Time, limit int) ([]string, error)
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
	// SelectUserCodes returns a user's usable codes, newest first, for
	// operators; codes are stored hashed, so only their existence shows.
	SelectUserCodes(ctx context.Context, userID string, now time.Time) ([]authdomain.Code, error)
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

// PasskeyStore reads and writes passkeys, their users' handles and started
// ceremonies (ADR-0044).
type PasskeyStore interface {
	// SetWebAuthnUserHandle sets a user's handle unless one is set, and
	// returns the stored handle, or ErrUserNotFound.
	SetWebAuthnUserHandle(ctx context.Context, userID string, handle []byte) ([]byte, error)
	// SelectUserByWebAuthnHandle returns the account a passkey names, or
	// ErrUserNotFound.
	SelectUserByWebAuthnHandle(ctx context.Context, handle []byte) (authdomain.User, error)
	InsertPasskey(ctx context.Context, p authdomain.Passkey) error
	// SelectPasskeys returns a user's passkeys, oldest first.
	SelectPasskeys(ctx context.Context, userID string) ([]authdomain.Passkey, error)
	SelectPasskeyByCredentialID(ctx context.Context, credentialID []byte) (authdomain.Passkey, bool, error)
	CountPasskeys(ctx context.Context, userID string) (int, error)
	// UpdatePasskeyUse stores a passkey's record, counter and backup state
	// after a sign-in.
	UpdatePasskeyUse(ctx context.Context, id string, record []byte, signCount int64, backupState bool, now time.Time) error
	// RenamePasskey and DeletePasskey report whether the user's passkey
	// exists.
	RenamePasskey(ctx context.Context, id, userID, name string) (bool, error)
	DeletePasskey(ctx context.Context, id, userID string) (bool, error)
	// DeletePasskeys removes every passkey of a user and returns how many.
	DeletePasskeys(ctx context.Context, userID string) (int64, error)
	InsertWebAuthnCeremony(ctx context.Context, c authdomain.WebAuthnCeremony) error
	// SelectWebAuthnCeremonyByTokenHash locks the ceremony with a token's
	// hash.
	SelectWebAuthnCeremonyByTokenHash(ctx context.Context, tokenHash []byte) (authdomain.WebAuthnCeremony, bool, error)
	ConsumeWebAuthnCeremony(ctx context.Context, id string, now time.Time) error
	DeleteOldWebAuthnCeremonies(ctx context.Context, before time.Time) (int64, error)
}

// SocialStore reads and writes Google and Apple identities, web sign-ins
// waiting for the provider, and native apps' nonces (ADR-0046).
type SocialStore interface {
	// VerifyEmailRemovePassword marks an address verified and removes the
	// account's password.
	VerifyEmailRemovePassword(ctx context.Context, userID string, now time.Time) error
	// InsertIdentity links an identity, or returns ErrIdentityTaken.
	InsertIdentity(ctx context.Context, i authdomain.Identity) error
	// SelectIdentity returns a provider subject's identity; lock locks it
	// until the transaction ends.
	SelectIdentity(ctx context.Context, provider, subject string, lock bool) (authdomain.Identity, bool, error)
	// SelectIdentities returns a user's identities, oldest first.
	SelectIdentities(ctx context.Context, userID string) ([]authdomain.Identity, error)
	CountIdentities(ctx context.Context, userID string) (int, error)
	UpdateIdentityUse(ctx context.Context, id, email string, privateEmail bool, now time.Time) error
	SetIdentityRefreshToken(ctx context.Context, id, keyID string, ciphertext []byte, clientID string) error
	// DeleteIdentity and DeleteIdentities return what they removed.
	DeleteIdentity(ctx context.Context, id, userID string) (authdomain.Identity, bool, error)
	DeleteIdentities(ctx context.Context, userID string) ([]authdomain.Identity, error)
	// SelectIdentitiesWithOtherKey returns up to limit identities whose
	// refresh token is encrypted with a key other than keyID.
	SelectIdentitiesWithOtherKey(ctx context.Context, keyID string, limit int) ([]authdomain.Identity, error)
	// UpdateIdentityRefreshKey replaces a refresh token still encrypted with
	// oldKeyID and reports whether it did.
	UpdateIdentityRefreshKey(ctx context.Context, id, oldKeyID, keyID string, ciphertext []byte) (bool, error)
	InsertOAuthState(ctx context.Context, s authdomain.OAuthState) error
	// SelectOAuthStateByTokenHash locks the web sign-in with a state's hash.
	SelectOAuthStateByTokenHash(ctx context.Context, tokenHash []byte) (authdomain.OAuthState, bool, error)
	ConsumeOAuthState(ctx context.Context, id string, now time.Time) error
	InsertSocialNonce(ctx context.Context, n authdomain.SocialNonce) error
	// UseSocialNonce uses up an unexpired nonce and reports whether it was
	// usable.
	UseSocialNonce(ctx context.Context, provider string, tokenHash []byte, now time.Time) (bool, error)
	// RecordAppleNotification remembers a notification until n.ExpiresAt,
	// stored used up, and reports false for one seen before.
	RecordAppleNotification(ctx context.Context, n authdomain.SocialNonce) (bool, error)
	DeleteOldSocialRequests(ctx context.Context, before time.Time) (int64, error)
	// InsertTokenRevocation queues a provider token for revocation.
	InsertTokenRevocation(ctx context.Context, r authdomain.TokenRevocation) error
	// ClaimTokenRevocations returns up to limit revocations due at now and
	// leases them until leaseUntil.
	ClaimTokenRevocations(ctx context.Context, now, leaseUntil time.Time, limit int) ([]authdomain.TokenRevocation, error)
	DeleteTokenRevocation(ctx context.Context, id string) error
	// RetryTokenRevocation records a failed revocation and when to try again.
	RetryTokenRevocation(ctx context.Context, id string, attempts int, next time.Time, lastError string) error
	// SelectTokenRevocationsWithOtherKey returns up to limit queued tokens
	// encrypted with a key other than keyID.
	SelectTokenRevocationsWithOtherKey(ctx context.Context, keyID string, limit int) ([]authdomain.TokenRevocation, error)
	// UpdateTokenRevocationKey replaces a queued token still encrypted with
	// oldKeyID and reports whether it did.
	UpdateTokenRevocationKey(ctx context.Context, id, oldKeyID, keyID string, ciphertext []byte) (bool, error)
}

// APIKeyStore reads and writes service accounts and API keys (ADR-0058). An
// empty orgID means the platform: a service account is found only with its
// own organisation, or with none for a platform one. Exactly one of userID
// and serviceAccountID names a key's owner.
type APIKeyStore interface {
	InsertServiceAccount(ctx context.Context, a authdomain.ServiceAccount) error
	// SelectServiceAccounts returns orgID's service accounts, oldest first.
	SelectServiceAccounts(ctx context.Context, orgID string) ([]authdomain.ServiceAccount, error)
	// SelectServiceAccount returns one of orgID's service accounts, or
	// ErrServiceAccountNotFound; lock locks it until the transaction ends.
	SelectServiceAccount(ctx context.Context, orgID, id string, lock bool) (authdomain.ServiceAccount, error)
	CountServiceAccounts(ctx context.Context, orgID string) (int, error)
	UpdateServiceAccount(ctx context.Context, a authdomain.ServiceAccount) error
	// DeleteServiceAccount removes a service account with its keys and
	// reports whether it existed.
	DeleteServiceAccount(ctx context.Context, orgID, id string) (bool, error)
	InsertAPIKey(ctx context.Context, k authdomain.APIKey) error
	// SelectAPIKeys returns an owner's keys, newest first.
	SelectAPIKeys(ctx context.Context, userID, serviceAccountID string) ([]authdomain.APIKey, error)
	// CountActiveAPIKeys counts an owner's keys neither revoked nor expired
	// at now.
	CountActiveAPIKeys(ctx context.Context, userID, serviceAccountID string, now time.Time) (int, error)
	// SelectAPIKeyByLookupID returns a key and, for a service account's key,
	// its service account; or ErrAPIKeyNotFound, also for a deleted user's
	// key.
	SelectAPIKeyByLookupID(ctx context.Context, lookupID string) (authdomain.APIKey, authdomain.ServiceAccount, error)
	// TouchAPIKey records a use at now unless one was recorded after
	// notSince.
	TouchAPIKey(ctx context.Context, id string, now, notSince time.Time) error
	// RevokeAPIKey revokes an owner's key and returns it, reporting whether
	// this call revoked it; or ErrAPIKeyNotFound.
	RevokeAPIKey(ctx context.Context, id, userID, serviceAccountID string, now time.Time, reason string) (authdomain.APIKey, bool, error)
	// RevokeOwnerAPIKeys revokes every key of an owner and returns how many.
	RevokeOwnerAPIKeys(ctx context.Context, userID, serviceAccountID string, now time.Time, reason string) (int64, error)
	// MarkAPIKeysExpired marks up to limit keys expired by now, not revoked
	// and not marked before, and returns them.
	MarkAPIKeysExpired(ctx context.Context, now time.Time, limit int) ([]authdomain.APIKey, error)
	// DeleteOldAPIKeys removes keys expired or revoked before before.
	DeleteOldAPIKeys(ctx context.Context, before time.Time) (int64, error)
}

// Store is every storage operation the use cases need, plus transactions.
type Store interface {
	UserStore
	SessionStore
	CodeStore
	RoleStore
	MFAStore
	PasskeyStore
	SocialStore
	APIKeyStore
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
