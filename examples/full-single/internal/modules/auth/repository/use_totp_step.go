package repository

import "context"

// Checking and recording the step in one statement stops two requests, even
// on two instances, from using the same code.
const useTOTPStepSQL = `
	UPDATE auth_totp SET last_used_step = $2
	WHERE user_id = $1 AND confirmed_at IS NOT NULL AND (last_used_step IS NULL OR last_used_step < $2)`

// UseTOTPStep records step as used when it is later than the last used step,
// and reports whether it was.
func (s *Store) UseTOTPStep(ctx context.Context, userID string, step int64) (bool, error) {
	tag, err := s.db.Exec(ctx, useTOTPStepSQL, userID, step)
	return tag.RowsAffected() == 1, err
}
