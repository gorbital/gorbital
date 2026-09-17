package repository

import "context"

//nolint:gosec // SQL text, not a credential
const updateTOTPSecretSQL = `UPDATE auth_totp SET key_id = $3, secret_ciphertext = $4 WHERE user_id = $1 AND key_id = $2`

// UpdateTOTPSecret replaces a secret still encrypted with oldKeyID, and
// reports whether it did.
func (s *Store) UpdateTOTPSecret(ctx context.Context, userID, oldKeyID, keyID string, ciphertext []byte) (bool, error) {
	tag, err := s.db.Exec(ctx, updateTOTPSecretSQL, userID, oldKeyID, keyID, ciphertext)
	return tag.RowsAffected() == 1, err
}
