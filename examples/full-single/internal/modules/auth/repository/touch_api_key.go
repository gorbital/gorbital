package repository

import (
	"context"
	"time"
)

// Another instance may have touched the key since it was read; $3 keeps
// the update to once per interval.
//
//nolint:gosec // SQL text, not a credential
const touchAPIKeySQL = `UPDATE auth_api_keys SET last_used_at = $2 WHERE id = $1 AND (last_used_at IS NULL OR last_used_at <= $3)`

// TouchAPIKey records that a key was used at now, unless it was used after
// notSince.
func (s *Store) TouchAPIKey(ctx context.Context, id string, now, notSince time.Time) error {
	_, err := s.db.Exec(ctx, touchAPIKeySQL, id, now, notSince)
	return err
}
