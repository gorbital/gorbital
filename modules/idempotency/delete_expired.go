package idempotency

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// deleteExpiredSQL deletes up to $3 keys claimed more than the retention ($2
// microseconds) before now ($1, NULL: the database clock), oldest first,
// through the created_at index.
const deleteExpiredSQL = `
	DELETE FROM idempotency_keys WHERE id IN (
		SELECT id FROM idempotency_keys
		WHERE created_at <= COALESCE($1::timestamptz, statement_timestamp()) - $2::bigint * interval '1 microsecond'
		ORDER BY created_at LIMIT $3
	)`

// DeleteExpired deletes up to limit keys older than the retention, with
// their stored responses, and returns how many it deleted. Apps run it from
// a periodic job until it deletes fewer than limit.
func (s *Store) DeleteExpired(ctx context.Context, limit int) (int64, error) {
	if limit < 1 {
		return 0, errors.New("idempotency: delete limit must be at least 1")
	}
	tag, err := s.pool.Exec(ctx, deleteExpiredSQL, s.clock(), s.retention(ctx).Microseconds(), limit)
	if err != nil {
		return 0, fmt.Errorf("idempotency: delete expired keys: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	return tag.RowsAffected(), nil
}

const selectOldestKeySQL = `SELECT min(created_at) FROM idempotency_keys`

// Oldest returns when the oldest stored key was claimed; ok is false when
// there are none.
func (s *Store) Oldest(ctx context.Context) (oldest time.Time, ok bool, err error) {
	var t *time.Time
	if err := s.pool.QueryRow(ctx, selectOldestKeySQL).Scan(&t); err != nil {
		return time.Time{}, false, fmt.Errorf("idempotency: oldest key: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	if t == nil {
		return time.Time{}, false, nil
	}
	return t.UTC(), true, nil
}
