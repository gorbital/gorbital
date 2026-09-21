package repository

import "context"

//nolint:gosec // SQL text, not a credential
const setIdentityRefreshTokenSQL = `
	UPDATE auth_identities SET refresh_key_id = $2, refresh_token_ciphertext = $3, refresh_client_id = $4
	WHERE id = $1`

// SetIdentityRefreshToken stores an identity's encrypted refresh token.
func (s *Store) SetIdentityRefreshToken(ctx context.Context, id, keyID string, ciphertext []byte, clientID string) error {
	_, err := s.db.Exec(ctx, setIdentityRefreshTokenSQL, id, keyID, ciphertext, clientID)
	return err
}
