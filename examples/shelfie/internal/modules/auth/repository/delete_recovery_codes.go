package repository

import "context"

const deleteRecoveryCodesSQL = `DELETE FROM auth_recovery_codes WHERE user_id = $1`

// DeleteRecoveryCodes removes every recovery code of a user.
func (s *Store) DeleteRecoveryCodes(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx, deleteRecoveryCodesSQL, userID)
	return err
}
