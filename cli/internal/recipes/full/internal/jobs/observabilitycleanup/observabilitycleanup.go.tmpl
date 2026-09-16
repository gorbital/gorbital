// Package observabilitycleanup runs the observability_cleanup background job:
// it deletes request minutes older than observability.retention, which
// /ops/observability reads (ADR-0064). Its schedule can be changed through
// /ops/jobs/definitions/observability_cleanup.
package observabilitycleanup

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// Name identifies the job. It is public API: renaming it orphans its
// configuration overrides and history.
const Name = "observability_cleanup"

// BatchSize is how many minutes one delete statement removes.
const BatchSize = 5_000

// Args are the job's arguments.
type Args struct{}

// Kind returns [Name].
func (Args) Kind() string { return Name }

// DeleteBefore removes up to limit minutes that started before before and
// returns how many it removed; observability.Store's DeleteBefore method
// matches it.
type DeleteBefore func(ctx context.Context, before time.Time, limit int) (int64, error)

// Worker runs observability_cleanup jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	deleteBefore DeleteBefore
	retention    func(context.Context) time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

// NewWorker returns a Worker deleting minutes older than retention, read
// when each run starts.
func NewWorker(deleteBefore DeleteBefore, retention func(context.Context) time.Duration, logger *slog.Logger) *Worker {
	return &Worker{deleteBefore: deleteBefore, retention: retention, logger: logger, now: time.Now}
}

// Work deletes old minutes in batches until none are left. An error retries
// the job.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	before := w.now().Add(-w.retention(ctx))
	var total int64
	for {
		n, err := w.deleteBefore(ctx, before, BatchSize)
		total += n
		if err != nil {
			return err
		}
		if n < BatchSize {
			break
		}
	}
	if total > 0 {
		w.logger.InfoContext(ctx, "old request minutes deleted", "job", Name, "job_id", job.ID, "minutes", total, "before", before)
	}
	return nil
}
