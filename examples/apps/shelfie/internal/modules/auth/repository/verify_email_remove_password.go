package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const verifyEmailRemovePasswordSQL = `
	UPDATE auth_users SET email_verified_at = $2, password_hash = NULL, password_changed_at = $2, updated_at = $2
	WHERE id = $1`

// VerifyEmailRemovePassword marks an unverified account's address verified
// by a provider and removes the password its unproven registrant chose
// (ADR-0046).
func (s *Store) VerifyEmailRemovePassword(ctx context.Context, userID string, now time.Time) error {
	_, err := s.db.Exec(ctx, verifyEmailRemovePasswordSQL, userID, now)
	return err
}
