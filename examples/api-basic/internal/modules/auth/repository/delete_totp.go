package repository

import "context"

const deleteTOTPSQL = `DELETE FROM auth_totp WHERE user_id = $1`

// DeleteTOTP removes a user's authenticator app secret and reports whether
// there was one.
func (s *Store) DeleteTOTP(ctx context.Context, userID string) (bool, error) {
	tag, err := s.db.Exec(ctx, deleteTOTPSQL, userID)
	return tag.RowsAffected() == 1, err
}
