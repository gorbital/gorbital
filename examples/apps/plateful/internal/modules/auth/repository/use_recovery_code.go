package repository

import (
	"context"
	"time"
)

const useRecoveryCodeSQL = `
	UPDATE auth_recovery_codes SET used_at = $3
	WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`

// UseRecoveryCode marks an unused recovery code used and reports whether it
// matched one.
func (s *Store) UseRecoveryCode(ctx context.Context, userID string, hash []byte, now time.Time) (bool, error) {
	tag, err := s.db.Exec(ctx, useRecoveryCodeSQL, userID, hash, now)
	return tag.RowsAffected() == 1, err
}
