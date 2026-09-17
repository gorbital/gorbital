// Package idempotencycleanup runs the idempotency_cleanup background job: it
// deletes idempotency keys and the responses stored with them once they are
// older than idempotency.retention (ADR-0060). Stored responses can hold
// personal data, so they aren't kept longer. Its schedule can be changed
// through /ops/jobs/definitions/idempotency_cleanup.
package idempotencycleanup

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

// Name identifies the job. It is public API: renaming it orphans its
// configuration overrides and history.
const Name = "idempotency_cleanup"

// BatchSize is how many keys one delete statement removes.
const BatchSize = 1_000

// Args are the job's arguments.
type Args struct{}

// Kind returns [Name].
func (Args) Kind() string { return Name }

// DeleteExpired removes up to limit expired keys and returns how many it
// removed; idempotency.Store's DeleteExpired method matches it.
type DeleteExpired func(ctx context.Context, limit int) (int64, error)

// Worker runs idempotency_cleanup jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	deleteExpired DeleteExpired
	logger        *slog.Logger
}

// NewWorker returns a Worker.
func NewWorker(deleteExpired DeleteExpired, logger *slog.Logger) *Worker {
	return &Worker{deleteExpired: deleteExpired, logger: logger}
}

// Work deletes expired keys in batches until none are left. An error retries
// the job.
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
		w.logger.InfoContext(ctx, "expired idempotency keys deleted", "job", Name, "job_id", job.ID, "keys", total)
	}
	return nil
}
