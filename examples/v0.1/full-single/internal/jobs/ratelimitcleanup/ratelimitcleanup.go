// Package ratelimitcleanup runs the ratelimit_cleanup background job: it
// deletes shared rate limit buckets whose keys are back to a full budget, so
// the table holds only recently limited keys (ADR-0052). Its schedule can be
// changed through /ops/jobs/definitions/ratelimit_cleanup.
package ratelimitcleanup

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

// Name identifies the job. It is public API: renaming it orphans its
// configuration overrides and history.
const Name = "ratelimit_cleanup"

// BatchSize is how many buckets one delete statement removes.
const BatchSize = 10_000

// Args are the job's arguments.
type Args struct{}

// Kind returns [Name].
func (Args) Kind() string { return Name }

// DeleteExpired removes up to limit expired buckets and returns how many it
// removed; ratelimitpg.Store's DeleteExpired method matches it.
type DeleteExpired func(ctx context.Context, limit int) (int64, error)

// Worker runs ratelimit_cleanup jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	deleteExpired DeleteExpired
	logger        *slog.Logger
}

// NewWorker returns a Worker.
func NewWorker(deleteExpired DeleteExpired, logger *slog.Logger) *Worker {
	return &Worker{deleteExpired: deleteExpired, logger: logger}
}

// Work deletes expired buckets in batches until none are left. An error
// retries the job.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	var total int64
	for {
		n, err := w.deleteExpired(ctx, BatchSize)
		total += n
		if err != nil {
			return err
		}
		if n < BatchSize {
			break
		}
	}
	if total > 0 {
		w.logger.InfoContext(ctx, "expired rate limit buckets deleted", "job", Name, "job_id", job.ID, "buckets", total)
	}
	return nil
}
