package repository

import "context"

//nolint:gosec // SQL text, not a credential
const renamePasskeySQL = `UPDATE auth_passkeys SET name = $3 WHERE id = $1 AND user_id = $2`

// RenamePasskey renames one of a user's passkeys and reports whether it
// exists.
func (s *Store) RenamePasskey(ctx context.Context, id, userID, name string) (bool, error) {
	tag, err := s.db.Exec(ctx, renamePasskeySQL, id, userID, name)
	return tag.RowsAffected() == 1, err
}
