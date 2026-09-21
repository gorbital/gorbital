package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const updatePasskeyUseSQL = `
	UPDATE auth_passkeys SET credential = $2, sign_count = $3, backup_state = $4, last_used_at = $5
	WHERE id = $1`

// UpdatePasskeyUse stores a passkey's record, counter and backup state after
// a sign-in.
func (s *Store) UpdatePasskeyUse(ctx context.Context, id string, record []byte, signCount int64, backupState bool, now time.Time) error {
	_, err := s.db.Exec(ctx, updatePasskeyUseSQL, id, string(record), signCount, backupState, now)
	return err
}
