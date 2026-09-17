package repository

import (
	"context"
	"time"
)

// The session with ID $2 is kept; an empty $2 keeps none.
const revokeUserSessionsSQL = `
	UPDATE auth_sessions SET revoked_at = $3, revoked_reason = $4
	WHERE user_id = $1 AND id <> $2 AND revoked_at IS NULL`

// RevokeUserSessions ends every active session of userID except keep, and
// returns how many ended.
func (s *Store) RevokeUserSessions(ctx context.Context, userID, keep string, now time.Time, reason string) (int64, error) {
	tag, err := s.db.Exec(ctx, revokeUserSessionsSQL, userID, keep, now, reason)
	return tag.RowsAffected(), err
}
