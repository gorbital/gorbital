package repository

import (
	"context"
	"time"
)

const setUserBanSQL = `UPDATE auth_users SET banned_at = $2, banned_reason = $3, updated_at = $4 WHERE id = $1 AND deleted_at IS NULL`

// SetUserBan bans an account (bannedAt set) or lifts the ban (nil).
func (s *Store) SetUserBan(ctx context.Context, userID string, bannedAt *time.Time, reason string, now time.Time) error {
	_, err := s.db.Exec(ctx, setUserBanSQL, userID, bannedAt, reason, now)
	return err
}
