package repository

import (
	"context"
	"time"
)

const markSessionMFAVerifiedSQL = `UPDATE auth_sessions SET mfa_verified_at = $2 WHERE id = $1`

// MarkSessionMFAVerified records that a session verified a second factor at
// now; the time starts auth.RecentVerification again.
func (s *Store) MarkSessionMFAVerified(ctx context.Context, sessionID string, now time.Time) error {
	_, err := s.db.Exec(ctx, markSessionMFAVerifiedSQL, sessionID, now)
	return err
}
