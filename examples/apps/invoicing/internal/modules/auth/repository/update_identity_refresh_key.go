package repository

import "context"

//nolint:gosec // SQL text, not a credential
const updateIdentityRefreshKeySQL = `
	UPDATE auth_identities SET refresh_key_id = $3, refresh_token_ciphertext = $4
	WHERE id = $1 AND refresh_key_id = $2`

// UpdateIdentityRefreshKey replaces a refresh token still encrypted with
// oldKeyID and reports whether it did.
func (s *Store) UpdateIdentityRefreshKey(ctx context.Context, id, oldKeyID, keyID string, ciphertext []byte) (bool, error) {
	tag, err := s.db.Exec(ctx, updateIdentityRefreshKeySQL, id, oldKeyID, keyID, ciphertext)
	return tag.RowsAffected() == 1, err
}
