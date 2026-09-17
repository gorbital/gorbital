package repository

import (
	"context"
	"time"
)

const failCodeSQL = `
	UPDATE auth_codes
	SET attempts = attempts + 1,
		consumed_at = CASE WHEN attempts + 1 >= max_attempts THEN $2 ELSE consumed_at END
	WHERE id = $1`

// FailCode counts a wrong guess and ends the code at its last attempt.
func (s *Store) FailCode(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, failCodeSQL, id, now)
	return err
}
