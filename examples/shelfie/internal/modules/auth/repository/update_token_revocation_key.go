package repository

import "context"

//nolint:gosec // SQL text, not a credential
const updateTokenRevocationKeySQL = `
	UPDATE auth_token_revocations SET key_id = $3, token_ciphertext = $4
	WHERE id = $1 AND key_id = $2`

// UpdateTokenRevocationKey replaces a queued token still encrypted with
// oldKeyID and reports whether it did.
func (s *Store) UpdateTokenRevocationKey(ctx context.Context, id, oldKeyID, keyID string, ciphertext []byte) (bool, error) {
	tag, err := s.db.Exec(ctx, updateTokenRevocationKeySQL, id, oldKeyID, keyID, ciphertext)
	return tag.RowsAffected() == 1, err
}
