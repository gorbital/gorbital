package repository

import (
	"context"
	"time"
)

//nolint:gosec // SQL text, not a credential
const deleteOldAPIKeysSQL = `DELETE FROM auth_api_keys WHERE expires_at < $1 OR revoked_at < $1`

// DeleteOldAPIKeys removes keys that expired or were revoked before before,
// and returns how many.
func (s *Store) DeleteOldAPIKeys(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, deleteOldAPIKeysSQL, before)
	return tag.RowsAffected(), err
}
