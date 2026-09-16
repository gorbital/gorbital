package flags

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// deleteHistoryBeforeSQL deletes the oldest changes first, one batch at a
// time. History grows by one row per change, so a scan per batch is cheap.
const deleteHistoryBeforeSQL = `
	DELETE FROM flags_history
	WHERE id IN (SELECT id FROM flags_history WHERE changed_at < $1 ORDER BY id LIMIT $2)`

// DeleteHistoryBefore deletes up to limit flag changes made before
// before, oldest first, and returns how many it deleted. Retention calls it
// until it deletes fewer than limit (ADR-0051).
func (s *Store) DeleteHistoryBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 {
		return 0, errors.New("flags: delete limit must be at least 1")
	}
	tag, err := s.pool.Exec(ctx, deleteHistoryBeforeSQL, before, limit)
	if err != nil {
		return 0, fmt.Errorf("flags: delete history: %w", err)
	}
	return tag.RowsAffected(), nil
}

const selectOldestHistorySQL = `SELECT min(changed_at) FROM flags_history`

// OldestHistory returns when the oldest recorded flag change was made; ok is
// false when there are none.
func (s *Store) OldestHistory(ctx context.Context) (oldest time.Time, ok bool, err error) {
	var t *time.Time
	if err := s.pool.QueryRow(ctx, selectOldestHistorySQL).Scan(&t); err != nil {
		return time.Time{}, false, fmt.Errorf("flags: oldest history: %w", err)
	}
	if t == nil {
		return time.Time{}, false, nil
	}
	return t.UTC(), true, nil
}
