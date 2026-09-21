package domain

import "time"

// MaxRevocationAttempts is how many times a provider token's revocation is
// tried before it is abandoned.
const MaxRevocationAttempts = 10

// TokenRevocation is a provider refresh token waiting to be revoked: queued
// in the transaction that unlinks its identity or deletes its account, and
// revoked by the auth_revoke_tokens job (ADR-0046). The token stays
// encrypted, bound to the identity's provider and subject.
type TokenRevocation struct {
	ID              string
	Provider        string
	Subject         string
	ClientID        string
	KeyID           string
	TokenCiphertext []byte
	// Attempts counts failed revocations; NextAttemptAt is when the next is
	// due.
	Attempts      int
	NextAttemptAt time.Time
	LastError     string
	CreatedAt     time.Time
}

// RevocationResult counts one run of the revocation job.
type RevocationResult struct {
	Revoked   int
	Retrying  int
	Abandoned int
}

// RevocationBackoff returns how long to wait after a revocation's
// attempts-th failure: a minute, doubling, at most 6 hours.
func RevocationBackoff(attempts int) time.Duration {
	return min(time.Minute<<min(max(attempts-1, 0), 10), 6*time.Hour)
}
