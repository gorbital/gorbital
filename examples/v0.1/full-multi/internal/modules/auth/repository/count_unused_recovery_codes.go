package repository

import "context"

const countUnusedRecoveryCodesSQL = `SELECT count(*) FROM auth_recovery_codes WHERE user_id = $1 AND used_at IS NULL`

// CountUnusedRecoveryCodes returns how many recovery codes a user has left.
func (s *Store) CountUnusedRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countUnusedRecoveryCodesSQL, userID).Scan(&n)
	return n, err
}
