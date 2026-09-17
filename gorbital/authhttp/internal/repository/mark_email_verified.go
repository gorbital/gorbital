package repository

import (
	"context"
	"time"
)

const markEmailVerifiedSQL = `
	UPDATE auth_users SET email_verified_at = COALESCE(email_verified_at, $2), updated_at = $2
	WHERE id = $1`

// MarkEmailVerified records that the user proved they own the address.
func (s *Store) MarkEmailVerified(ctx context.Context, userID string, now time.Time) error {
	_, err := s.db.Exec(ctx, markEmailVerifiedSQL, userID, now)
	return err
}
