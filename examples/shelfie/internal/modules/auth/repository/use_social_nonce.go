package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const useSocialNonceSQL = `
	UPDATE auth_social_nonces SET consumed_at = $3
	WHERE token_hash = $2 AND provider = $1 AND consumed_at IS NULL AND expires_at > $3`

// UseSocialNonce uses up a provider's unexpired nonce and reports whether it
// was usable.
func (s *Store) UseSocialNonce(ctx context.Context, provider string, tokenHash []byte, now time.Time) (bool, error) {
	tag, err := s.db.Exec(ctx, useSocialNonceSQL, provider, tokenHash, now)
	return tag.RowsAffected() == 1, err
}
