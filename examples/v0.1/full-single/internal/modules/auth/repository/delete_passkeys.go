package repository

import "context"

//nolint:gosec // SQL text, not a credential
const deletePasskeysSQL = `DELETE FROM auth_passkeys WHERE user_id = $1`

// DeletePasskeys removes every passkey of a user and returns how many.
func (s *Store) DeletePasskeys(ctx context.Context, userID string) (int64, error) {
	tag, err := s.db.Exec(ctx, deletePasskeysSQL, userID)
	return tag.RowsAffected(), err
}
