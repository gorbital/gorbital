package repository

import (
	"context"
	"time"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

const failCodeSQL = `
	UPDATE phone_codes SET attempts = attempts + 1,
		used_at = CASE WHEN attempts + 1 >= $3 THEN $2 ELSE used_at END
	WHERE id = $1`

// FailCode counts a wrong guess, using the code up at its last attempt.
func (s *Store) FailCode(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, failCodeSQL, id, now, domain.CodeAttempts)
	return err
}
