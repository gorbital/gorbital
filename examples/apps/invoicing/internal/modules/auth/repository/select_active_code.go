package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	authdomain "example.com/invoicing/internal/modules/auth/domain"
)

const selectActiveCodeSQL = `
	SELECT id, user_id, purpose, code_hash, attempts, max_attempts, expires_at, created_at
	FROM auth_codes
	WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL AND expires_at > $3 AND attempts < max_attempts
	ORDER BY created_at DESC LIMIT 1
	FOR UPDATE`

// SelectActiveCode locks the newest usable code of a purpose.
func (s *Store) SelectActiveCode(ctx context.Context, userID, purpose string, now time.Time) (authdomain.Code, bool, error) {
	var c authdomain.Code
	err := s.db.QueryRow(ctx, selectActiveCodeSQL, userID, purpose, now).Scan(
		&c.ID, &c.UserID, &c.Purpose, &c.Hash, &c.Attempts, &c.MaxAttempts, &c.ExpiresAt, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return authdomain.Code{}, false, nil
	}
	return c, err == nil, err
}
