package repository

import (
	"context"
	"time"
)

const failMFAChallengeSQL = `
	UPDATE auth_mfa_challenges
	SET attempts = attempts + 1,
		consumed_at = CASE WHEN attempts + 1 >= max_attempts THEN $2 ELSE consumed_at END
	WHERE id = $1`

// FailMFAChallenge counts a wrong second factor and ends the challenge at its
// last attempt.
func (s *Store) FailMFAChallenge(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, failMFAChallengeSQL, id, now)
	return err
}
