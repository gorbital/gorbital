package repository

import (
	"context"
	"time"
)

const insertRecoveryCodesSQL = `
	INSERT INTO auth_recovery_codes (id, user_id, code_hash, created_at)
	SELECT c.id, $1, c.code_hash, $4
	FROM unnest($2::text[], $3::bytea[]) AS c (id, code_hash)`

// ReplaceRecoveryCodes deletes a user's recovery codes and stores new hashes.
// Call it in a transaction.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID string, ids []string, hashes [][]byte, now time.Time) error {
	if err := s.DeleteRecoveryCodes(ctx, userID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, insertRecoveryCodesSQL, userID, ids, hashes, now)
	return err
}
