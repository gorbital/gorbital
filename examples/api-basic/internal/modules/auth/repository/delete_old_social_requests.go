package repository

import (
	"context"
	"time"
)

const deleteOldSocialRequestsSQL = `
	WITH states AS (DELETE FROM auth_oauth_states WHERE expires_at < $1 RETURNING 1),
	     nonces AS (DELETE FROM auth_social_nonces WHERE expires_at < $1 RETURNING 1)
	SELECT (SELECT count(*) FROM states) + (SELECT count(*) FROM nonces)`

// DeleteOldSocialRequests removes web sign-in states and native nonces that
// expired before before.
func (s *Store) DeleteOldSocialRequests(ctx context.Context, before time.Time) (int64, error) {
	var n int64
	err := s.db.QueryRow(ctx, deleteOldSocialRequestsSQL, before).Scan(&n)
	return n, err
}
