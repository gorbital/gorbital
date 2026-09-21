package repository

import (
	"context"
	"time"
)

const consumeMFAChallengeSQL = `UPDATE auth_mfa_challenges SET consumed_at = $2 WHERE id = $1`

// ConsumeMFAChallenge marks a finished sign-in's challenge as used.
func (s *Store) ConsumeMFAChallenge(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, consumeMFAChallengeSQL, id, now)
	return err
}
