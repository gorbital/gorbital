package auditpg_test

import (
	"context"
	"testing"
	"time"

	"apistock.dev/actor"
	"apistock.dev/audit"
	"apistock.dev/modules/auditpg"
)

func TestDeleteBeforeAndOldest(t *testing.T) {
	store, _ := newStore(t)
	ctx := actor.With(context.Background(), actor.System("test"))
	if _, ok, err := store.Oldest(ctx); err != nil || ok {
		t.Errorf("Oldest() on an empty log = %v, %v; want none", ok, err)
	}
	t0 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 7 {
		if err := store.Record(ctx, audit.Event{OccurredAt: t0.Add(time.Duration(i) * 24 * time.Hour), Action: "test.happened", Outcome: audit.OutcomeSuccess}); err != nil {
			t.Fatal(err)
		}
	}
	if oldest, ok, err := store.Oldest(ctx); err != nil || !ok || !oldest.Equal(t0) {
		t.Errorf("Oldest() = %v, %v, %v; want %v", oldest, ok, err, t0)
	}

	// Five events are older than day 5; batches of 2 delete them oldest first.
	cutoff := t0.Add(5 * 24 * time.Hour)
	var deleted []int64
	for {
		n, err := store.DeleteBefore(ctx, cutoff, 2)
		if err != nil {
			t.Fatal(err)
		}
		deleted = append(deleted, n)
		if n < 2 {
			break
		}
	}
	if len(deleted) != 3 || deleted[0] != 2 || deleted[1] != 2 || deleted[2] != 1 {
		t.Errorf("batches deleted %v, want [2 2 1]", deleted)
	}
	page, err := store.List(ctx, auditpg.Filter{})
	if err != nil || len(page.Events) != 2 || page.Events[1].OccurredAt.Before(cutoff) {
		t.Errorf("after deleting, %d events left (%v), want the 2 on or after the cutoff", len(page.Events), err)
	}
	if oldest, _, _ := store.Oldest(ctx); !oldest.Equal(cutoff) {
		t.Errorf("Oldest() after deleting = %v, want %v", oldest, cutoff)
	}
	if _, err := store.DeleteBefore(ctx, cutoff, 0); err == nil {
		t.Error("DeleteBefore(limit 0) error = nil")
	}
}
