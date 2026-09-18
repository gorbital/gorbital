package repository

import (
	"context"
	"time"
)

const revokeSessionSQL = `
	UPDATE auth_sessions SET revoked_at = $3, revoked_reason = $4
	WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`

// RevokeSession ends one active session of userID and reports whether it
// was active.
func (s *Store) RevokeSession(ctx context.Context, id, userID string, now time.Time, reason string) (bool, error) {
	tag, err := s.db.Exec(ctx, revokeSessionSQL, id, userID, now, reason)
	return tag.RowsAffected() == 1, err
}
