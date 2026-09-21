package repository

import "context"

//nolint:gosec // SQL text, not a credential
const deletePasskeySQL = `DELETE FROM auth_passkeys WHERE id = $1 AND user_id = $2`

// DeletePasskey removes one of a user's passkeys and reports whether it
// existed.
func (s *Store) DeletePasskey(ctx context.Context, id, userID string) (bool, error) {
	tag, err := s.db.Exec(ctx, deletePasskeySQL, id, userID)
	return tag.RowsAffected() == 1, err
}
