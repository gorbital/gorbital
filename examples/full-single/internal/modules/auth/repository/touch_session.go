package repository

import (
	"context"
	"time"
)

const touchSessionSQL = `
	UPDATE auth_sessions SET last_seen_at = $2, idle_expires_at = $3
	WHERE id = $1 AND revoked_at IS NULL`

// TouchSession records a use of the session and extends its idle expiry.
func (s *Store) TouchSession(ctx context.Context, id string, now, idleExpiresAt time.Time) error {
	_, err := s.db.Exec(ctx, touchSessionSQL, id, now, idleExpiresAt)
	return err
}
