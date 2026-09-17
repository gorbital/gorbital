package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const retryTokenRevocationSQL = `
	UPDATE auth_token_revocations SET attempts = $2, next_attempt_at = $3, last_error = $4
	WHERE id = $1`

// RetryTokenRevocation records a failed revocation and when to try again.
func (s *Store) RetryTokenRevocation(ctx context.Context, id string, attempts int, next time.Time, lastError string) error {
	_, err := s.db.Exec(ctx, retryTokenRevocationSQL, id, attempts, next, lastError)
	return err
}
