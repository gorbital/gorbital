package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const updatePasswordSQL = `
	UPDATE auth_users SET password_hash = $2, password_changed_at = $3, updated_at = $3
	WHERE id = $1`

// UpdatePassword replaces a user's password hash.
func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash string, now time.Time) error {
	_, err := s.db.Exec(ctx, updatePasswordSQL, userID, passwordHash, now)
	return err
}
