package auditpg

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// deleteEventsBeforeSQL deletes the oldest events first, one batch at a time,
// through the audit_events_occurred_at index, so no statement holds locks
// for long.
const deleteEventsBeforeSQL = `
	DELETE FROM audit_events
	WHERE id IN (SELECT id FROM audit_events WHERE occurred_at < $1 ORDER BY occurred_at LIMIT $2)`

// DeleteBefore deletes up to limit events that occurred before before,
// oldest first, and returns how many it deleted. Retention calls it until it
// deletes fewer than limit (ADR-0051).
func (s *Store) DeleteBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 {
		return 0, errors.New("auditpg: delete limit must be at least 1")
	}
	tag, err := s.pool.Exec(ctx, deleteEventsBeforeSQL, before, limit)
	if err != nil {
		return 0, fmt.Errorf("auditpg: delete events: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	return tag.RowsAffected(), nil
}

const selectOldestEventSQL = `SELECT min(occurred_at) FROM audit_events`

// Oldest returns when the oldest stored event occurred; ok is false when
// there are none.
func (s *Store) Oldest(ctx context.Context) (oldest time.Time, ok bool, err error) {
	var t *time.Time
	if err := s.pool.QueryRow(ctx, selectOldestEventSQL).Scan(&t); err != nil {
		return time.Time{}, false, fmt.Errorf("auditpg: oldest event: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	if t == nil {
		return time.Time{}, false, nil
	}
	return t.UTC(), true, nil
}
