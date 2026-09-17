package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const countActiveAPIKeysSQL = `
	SELECT count(*) FROM auth_api_keys
	WHERE (user_id = NULLIF($1, '') OR service_account_id = NULLIF($2, '')) AND revoked_at IS NULL AND expires_at > $3`

// CountActiveAPIKeys returns how many usable keys a user or a service
// account has at now.
func (s *Store) CountActiveAPIKeys(ctx context.Context, userID, serviceAccountID string, now time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countActiveAPIKeysSQL, userID, serviceAccountID, now).Scan(&n)
	return n, err
}
