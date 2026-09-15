package settings_test

import (
	"encoding/json"
	"testing"
	"time"

	"apistock.dev/modules/postgres/pgtest"
	"apistock.dev/modules/settings"
)

func TestDeleteHistoryBeforeAndOldest(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	store, _ := newStore(t, pool, declare())
	if _, ok, err := store.OldestHistory(ctx); err != nil || ok {
		t.Errorf("OldestHistory() without changes = %v, %v; want none", ok, err)
	}
	for i, value := range []string{`"sync"`, `"async"`, `"sync"`} {
		if _, err := store.Set(ctx, "mail.delivery", json.RawMessage(value), settings.Change{Version: int64(i)}); err != nil {
			t.Fatalf("Set(%s) error = %v", value, err)
		}
	}
	// Make the first two changes a year old.
	if _, err := pool.Exec(ctx, `UPDATE settings_history SET changed_at = now() - interval '365 days' WHERE version <= 2`); err != nil {
		t.Fatal(err)
	}
	if oldest, ok, err := store.OldestHistory(ctx); err != nil || !ok || time.Since(oldest) < 364*24*time.Hour {
		t.Errorf("OldestHistory() = %v, %v, %v; want a year ago", oldest, ok, err)
	}

	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	if n, err := store.DeleteHistoryBefore(ctx, cutoff, 1); err != nil || n != 1 {
		t.Errorf("DeleteHistoryBefore(limit 1) = %d, %v; want 1", n, err)
	}
	if n, err := store.DeleteHistoryBefore(ctx, cutoff, 10); err != nil || n != 1 {
		t.Errorf("DeleteHistoryBefore(limit 10) = %d, %v; want the other old change", n, err)
	}
	history, err := store.History(ctx, "mail.delivery", 0, 10)
	if err != nil || len(history) != 1 || history[0].Version != 3 {
		t.Errorf("History() after deleting = %+v, %v; want only version 3", history, err)
	}
	if _, err := store.DeleteHistoryBefore(ctx, cutoff, 0); err == nil {
		t.Error("DeleteHistoryBefore(limit 0) error = nil")
	}
}
