package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

const selectActiveSessionsSQL = `
	SELECT ` + sessionColumns + `
	FROM auth_sessions
	WHERE user_id = $1 AND revoked_at IS NULL AND idle_expires_at > $2 AND absolute_expires_at > $2
	ORDER BY last_seen_at DESC, id`

// SelectActiveSessions returns a user's sessions that haven't ended, most
// recently used first.
func (s *Store) SelectActiveSessions(ctx context.Context, userID string, now time.Time) ([]authdomain.Session, error) {
	rows, err := s.db.Query(ctx, selectActiveSessionsSQL, userID, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanSession)
}
