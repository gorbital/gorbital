package repository

import (
	"context"

	authdomain "gorbital.dev/gorbital/authhttp/internal/domain"
)

//nolint:gosec // SQL text, not a credential
const insertSocialNonceSQL = `
	INSERT INTO auth_social_nonces (id, token_hash, provider, expires_at, created_at)
	VALUES ($1, $2, $3, $4, $5)`

// InsertSocialNonce stores a nonce given to a native app.
func (s *Store) InsertSocialNonce(ctx context.Context, n authdomain.SocialNonce) error {
	_, err := s.db.Exec(ctx, insertSocialNonceSQL, n.ID, n.TokenHash, n.Provider, n.ExpiresAt, n.CreatedAt)
	return err
}
