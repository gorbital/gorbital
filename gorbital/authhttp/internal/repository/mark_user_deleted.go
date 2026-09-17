package repository

import (
	"context"
	"time"
)

const markUserDeletedSQL = `
	UPDATE auth_users SET deleted_at = $2, updated_at = $2
	WHERE id = $1 AND deleted_at IS NULL`

// MarkUserDeleted soft-deletes an account; cleanup removes it later.
func (s *Store) MarkUserDeleted(ctx context.Context, userID string, now time.Time) error {
	_, err := s.db.Exec(ctx, markUserDeletedSQL, userID, now)
	return err
}
