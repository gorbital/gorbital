package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const removePasswordSQL = `
	UPDATE auth_users SET password_hash = NULL, password_changed_at = $2, updated_at = $2
	WHERE id = $1`

// RemovePassword removes a user's password.
func (s *Store) RemovePassword(ctx context.Context, userID string, now time.Time) error {
	_, err := s.db.Exec(ctx, removePasswordSQL, userID, now)
	return err
}
