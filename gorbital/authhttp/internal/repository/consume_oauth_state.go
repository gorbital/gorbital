package repository

import (
	"context"
	"time"
)

const consumeOAuthStateSQL = `UPDATE auth_oauth_states SET consumed_at = $2 WHERE id = $1 AND consumed_at IS NULL`

// ConsumeOAuthState ends a web sign-in so its state can't be used again.
func (s *Store) ConsumeOAuthState(ctx context.Context, id string, now time.Time) error {
	_, err := s.db.Exec(ctx, consumeOAuthStateSQL, id, now)
	return err
}
