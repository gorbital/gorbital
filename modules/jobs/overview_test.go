package jobs_test

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

func TestOverview(t *testing.T) {
	s := newManager(t, newPool(t))
	ctx := context.Background()

	// Scheduled an hour ahead, so the running worker leaves it alone.
	if _, err := s.client.Insert(ctx, cleanupArgs{}, &river.InsertOpts{ScheduledAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	// A run of rebuild_index that ran out of attempts.
	res, err := s.client.Insert(ctx, rebuildArgs{}, &river.InsertOpts{ScheduledAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE river_job SET state = 'discarded', finalized_at = now() WHERE id = $1`, res.Job.ID); err != nil {
		t.Fatal(err)
	}

	o, err := s.manager.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Queues) != 1 {
		t.Fatalf("queues = %+v, want the default queue", o.Queues)
	}
	q := o.Queues[0]
	if q.Name != river.QueueDefault || !q.Active || q.Paused || q.Scheduled != 1 || q.DiscardedLastDay != 1 || q.Available != 0 || q.Running != 0 {
		t.Errorf("default queue = %+v, want active with 1 scheduled and 1 discarded", q)
	}
	if len(o.Failing) != 1 || o.Failing[0].Name != "rebuild_index" || o.Failing[0].LastRun == nil {
		t.Errorf("failing = %+v, want rebuild_index with its last run", o.Failing)
	}

	if err := s.manager.PauseQueueWithReason(operator(), river.QueueDefault, "test"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the queue to show as paused", func() bool {
		o, err := s.manager.Overview(ctx)
		return err == nil && len(o.Queues) == 1 && o.Queues[0].Paused
	})
}
