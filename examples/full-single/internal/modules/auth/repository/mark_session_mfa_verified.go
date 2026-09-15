package repository

import (
	"context"
	"time"
)

const markSessionMFAVerifiedSQL = `UPDATE auth_sessions SET mfa_verified_at = $2 WHERE id = $1 AND mfa_verified_at IS NULL`

// MarkSessionMFAVerified records that a session was verified with a second
// factor.
func (s *Store) MarkSessionMFAVerified(ctx context.Context, sessionID string, now time.Time) error {
	_, err := s.db.Exec(ctx, markSessionMFAVerifiedSQL, sessionID, now)
	return err
}
