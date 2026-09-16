package observability

import (
	"context"
	"errors"
	"time"
)

// deleteMinutesSQL deletes up to $2 minutes that started before $1, oldest
// first, through the primary key.
const deleteMinutesSQL = `
	DELETE FROM observability_minutes WHERE (minute, instance_id, method, route) IN (
		SELECT minute, instance_id, method, route FROM observability_minutes
		WHERE minute < $1 ORDER BY minute LIMIT $2
	)`

// DeleteBefore deletes up to limit minutes that started before before, and
// returns how many it deleted. Apps run it from a periodic job until it
// deletes fewer than limit.
func (s *Store) DeleteBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 {
		return 0, errors.New("observability: delete limit must be at least 1")
	}
	tag, err := s.pool.Exec(ctx, deleteMinutesSQL, before.UTC(), limit)
	if err != nil {
		return 0, dbError("delete minutes", err)
	}
	return tag.RowsAffected(), nil
}

const selectOldestMinuteSQL = `SELECT min(minute) FROM observability_minutes`

// Oldest returns the start of the oldest stored minute; ok is false when
// there are none.
func (s *Store) Oldest(ctx context.Context) (oldest time.Time, ok bool, err error) {
	var t *time.Time
	if err := s.pool.QueryRow(ctx, selectOldestMinuteSQL).Scan(&t); err != nil {
		return time.Time{}, false, dbError("oldest minute", err)
	}
	if t == nil {
		return time.Time{}, false, nil
	}
	return t.UTC(), true, nil
}
