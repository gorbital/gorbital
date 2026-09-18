package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

const selectUserCodesSQL = `SELECT id, user_id, purpose, code_hash, attempts, max_attempts, expires_at, created_at
FROM auth_codes WHERE user_id = $1 AND consumed_at IS NULL AND expires_at > $2
ORDER BY created_at DESC`

// SelectUserCodes returns a user's usable codes, newest first, without
// the codes themselves (only hashes are stored).
func (s *Store) SelectUserCodes(ctx context.Context, userID string, now time.Time) ([]authdomain.Code, error) {
	rows, err := s.db.Query(ctx, selectUserCodesSQL, userID, now)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (authdomain.Code, error) {
		var c authdomain.Code
		err := row.Scan(&c.ID, &c.UserID, &c.Purpose, &c.Hash, &c.Attempts, &c.MaxAttempts, &c.ExpiresAt, &c.CreatedAt)
		c.ExpiresAt, c.CreatedAt = c.ExpiresAt.UTC(), c.CreatedAt.UTC()
		return c, err
	})
}
