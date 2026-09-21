package repository

import (
	"context"
	"time"
)

// UseCode marks a code used.
func (s *Store) UseCode(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, `UPDATE phone_codes SET used_at = $2 WHERE id = $1`, id, now)
	return err
}
