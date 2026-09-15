package repository

import (
	"context"
	"time"
)

const consumeCodeSQL = `UPDATE auth_codes SET consumed_at = $2 WHERE id = $1`

// ConsumeCode marks a matched code as used.
func (s *Store) ConsumeCode(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, consumeCodeSQL, id, now)
	return err
}
