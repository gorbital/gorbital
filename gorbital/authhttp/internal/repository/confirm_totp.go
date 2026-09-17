package repository

import (
	"context"
	"time"
)

const confirmTOTPSQL = `UPDATE auth_totp SET confirmed_at = $2, last_used_step = $3 WHERE user_id = $1 AND confirmed_at IS NULL`

// ConfirmTOTP turns a user's secret on, recording step as used.
func (s *Store) ConfirmTOTP(ctx context.Context, userID string, step int64, now time.Time) error {
	_, err := s.db.Exec(ctx, confirmTOTPSQL, userID, now, step)
	return err
}
