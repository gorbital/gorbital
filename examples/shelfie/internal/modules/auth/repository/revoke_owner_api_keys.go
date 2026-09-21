package repository

import (
	"context"
	"time"
)

// Exactly one of $1 (a user) and $2 (a service account) is set.
//
//nolint:gosec // SQL text, not a credential
const revokeOwnerAPIKeysSQL = `
	UPDATE auth_api_keys SET revoked_at = $3, revoked_reason = $4
	WHERE (user_id = NULLIF($1, '') OR service_account_id = NULLIF($2, '')) AND revoked_at IS NULL`

// RevokeOwnerAPIKeys revokes every key of a user or a service account, and
// returns how many it revoked.
func (s *Store) RevokeOwnerAPIKeys(ctx context.Context, userID, serviceAccountID string, now time.Time, reason string) (int64, error) {
	tag, err := s.db.Exec(ctx, revokeOwnerAPIKeysSQL, userID, serviceAccountID, now, reason)
	return tag.RowsAffected(), err
}
