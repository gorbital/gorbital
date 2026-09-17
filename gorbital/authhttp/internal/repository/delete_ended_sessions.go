package repository

import (
	"context"
	"time"
)

const deleteEndedSessionsSQL = `
	DELETE FROM auth_sessions
	WHERE revoked_at < $1 OR idle_expires_at < $1 OR absolute_expires_at < $1`

// DeleteEndedSessions removes sessions that ended before before.
func (s *Store) DeleteEndedSessions(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, deleteEndedSessionsSQL, before)
	return tag.RowsAffected(), err
}
