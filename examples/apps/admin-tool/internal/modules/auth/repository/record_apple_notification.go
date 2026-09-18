package repository

import (
	"context"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

//nolint:gosec // SQL text, not a credential
const recordAppleNotificationSQL = `
	INSERT INTO auth_social_nonces (id, token_hash, provider, expires_at, consumed_at, created_at)
	VALUES ($1, $2, $3, $4, $5, $5)
	ON CONFLICT (token_hash) DO NOTHING`

// RecordAppleNotification remembers a notification's hash, used up at once
// so it can never serve as a nonce, and reports whether it was new.
func (s *Store) RecordAppleNotification(ctx context.Context, n authdomain.SocialNonce) (bool, error) {
	tag, err := s.db.Exec(ctx, recordAppleNotificationSQL, n.ID, n.TokenHash, n.Provider, n.ExpiresAt, n.CreatedAt)
	return tag.RowsAffected() == 1, err
}
