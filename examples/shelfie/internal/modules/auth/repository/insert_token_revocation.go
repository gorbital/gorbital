package repository

import (
	"context"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const insertTokenRevocationSQL = `
	INSERT INTO auth_token_revocations (id, provider, subject, client_id, key_id, token_ciphertext, next_attempt_at, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

// InsertTokenRevocation queues a provider token for revocation.
func (s *Store) InsertTokenRevocation(ctx context.Context, r authdomain.TokenRevocation) error {
	_, err := s.db.Exec(ctx, insertTokenRevocationSQL, r.ID, r.Provider, r.Subject, r.ClientID, r.KeyID, r.TokenCiphertext, r.NextAttemptAt, r.CreatedAt)
	return err
}
