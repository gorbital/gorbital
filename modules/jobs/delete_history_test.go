package jobs_test

import (
	"testing"
	"time"

	"gorbital.dev/modules/jobs"
)

func TestDeleteHistoryBeforeAndOldest(t *testing.T) {
	ctx := operator()
	s := newManager(t, newPool(t))
	if _, ok, err := s.manager.OldestHistory(ctx); err != nil || ok {
		t.Errorf("OldestHistory() without changes = %v, %v; want none", ok, err)
	}
	for i := range 3 {
		if _, err := s.manager.Update(ctx, "rebuild_index", jobs.ConfigPatch{MaxAttempts: ptr(4 + i)}, jobs.Change{Version: int64(i), Reason: "test"}); err != nil {
			t.Fatalf("Update() error = %v", err)
		}
	}
	// Make the first two changes a year old.
	if _, err := s.pool.Exec(ctx, `UPDATE jobs_definition_history SET changed_at = now() - interval '365 days' WHERE version <= 2`); err != nil {
		t.Fatal(err)
	}
	if oldest, ok, err := s.manager.OldestHistory(ctx); err != nil || !ok || time.Since(oldest) < 364*24*time.Hour {
		t.Errorf("OldestHistory() = %v, %v, %v; want a year ago", oldest, ok, err)
	}

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	if n, err := s.manager.DeleteHistoryBefore(ctx, cutoff, 1); err != nil || n != 1 {
		t.Errorf("DeleteHistoryBefore(limit 1) = %d, %v; want 1", n, err)
	}
	if n, err := s.manager.DeleteHistoryBefore(ctx, cutoff, 10); err != nil || n != 1 {
		t.Errorf("DeleteHistoryBefore(limit 10) = %d, %v; want the other old change", n, err)
	}
	changes, err := s.manager.History(ctx, "rebuild_index", 0, 10)
	if err != nil || len(changes) != 1 || changes[0].Version != 3 {
		t.Errorf("History() after deleting = %+v, %v; want only version 3", changes, err)
	}
	if _, err := s.manager.DeleteHistoryBefore(ctx, cutoff, 0); err == nil {
		t.Error("DeleteHistoryBefore(limit 0) error = nil")
	}
}
