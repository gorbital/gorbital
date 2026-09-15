package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// deleteDefinitionHistoryBeforeSQL deletes the oldest changes first, one
// batch at a time. History grows by one row per configuration change, so a
// scan per batch is cheap.
const deleteDefinitionHistoryBeforeSQL = `
	DELETE FROM jobs_definition_history
	WHERE id IN (SELECT id FROM jobs_definition_history WHERE changed_at < $1 ORDER BY id LIMIT $2)`

// DeleteHistoryBefore deletes up to limit job configuration changes made
// before before, oldest first, and returns how many it deleted. Retention
// calls it until it deletes fewer than limit (ADR-0051).
func (m *Manager) DeleteHistoryBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit < 1 {
		return 0, errors.New("jobs: delete limit must be at least 1")
	}
	tag, err := m.pool.Exec(ctx, deleteDefinitionHistoryBeforeSQL, before, limit)
	if err != nil {
		return 0, fmt.Errorf("jobs: delete definition history: %w", err)
	}
	return tag.RowsAffected(), nil
}

const selectOldestDefinitionHistorySQL = `SELECT min(changed_at) FROM jobs_definition_history`

// OldestHistory returns when the oldest recorded configuration change was
// made; ok is false when there are none.
func (m *Manager) OldestHistory(ctx context.Context) (oldest time.Time, ok bool, err error) {
	var t *time.Time
	if err := m.pool.QueryRow(ctx, selectOldestDefinitionHistorySQL).Scan(&t); err != nil {
		return time.Time{}, false, fmt.Errorf("jobs: oldest definition history: %w", err)
	}
	if t == nil {
		return time.Time{}, false, nil
	}
	return t.UTC(), true, nil
}
