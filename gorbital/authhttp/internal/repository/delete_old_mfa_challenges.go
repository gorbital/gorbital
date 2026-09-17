package repository

import (
	"context"
	"time"
)

const deleteOldMFAChallengesSQL = `DELETE FROM auth_mfa_challenges WHERE expires_at < $1`

// DeleteOldMFAChallenges removes challenges that expired before before.
func (s *Store) DeleteOldMFAChallenges(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, deleteOldMFAChallengesSQL, before)
	return tag.RowsAffected(), err
}
