// Package retention runs the retention background job: it deletes audit
// events and configuration history older than their retention runtime
// settings, in small batches, and records an audit event for each kind of
// data it deleted (ADR-0051). Its schedule can be changed through
// /ops/jobs/definitions/retention.
package retention

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"apistock.dev/actor"
	"apistock.dev/audit"
)

// Name identifies the job. It is public API: renaming it orphans its
// configuration overrides and history.
const Name = "retention"

// BatchSize is how many rows one delete statement removes.
const BatchSize = 5000

// Args are the job's arguments.
type Args struct{}

// Kind returns [Name].
func (Args) Kind() string { return Name }

// A Target is one kind of data with a retention.
type Target struct {
	// Name identifies the data in logs and audit events, such as
	// audit_events.
	Name string
	// Retention returns how long rows are kept, usually a runtime setting.
	Retention func(context.Context) time.Duration
	// Delete removes up to limit rows older than before and returns how many
	// it removed.
	Delete func(ctx context.Context, before time.Time, limit int) (int64, error)
}

// Worker runs retention jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	targets  []Target
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
}

// NewWorker returns a Worker for targets.
func NewWorker(targets []Target, recorder audit.Recorder, logger *slog.Logger) *Worker {
	return &Worker{targets: targets, recorder: recorder, logger: logger, now: time.Now}
}

// Work deletes each target's expired rows. A failing target doesn't stop the
// others; the job fails, and retries, when any did.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	ctx = actor.With(ctx, actor.System(Name))
	var errs []error
	for _, target := range w.targets {
		before := w.now().Add(-target.Retention(ctx)).UTC()
		deleted, err := purge(ctx, target, before)
		if deleted > 0 {
			w.logger.InfoContext(ctx, "expired data deleted", "job", Name, "job_id", job.ID, "data", target.Name, "rows", deleted, "before", before)
			// Shortened retention stays visible after the rows are gone.
			if recErr := w.recorder.Record(ctx, audit.Event{
				Action: "retention.purged", Outcome: audit.OutcomeSuccess, ResourceType: "data", ResourceID: target.Name,
				Metadata: map[string]any{"rows": deleted, "before": before.Format(time.RFC3339)},
			}); recErr != nil {
				errs = append(errs, recErr)
			}
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// purge deletes target's rows older than before, one batch at a time, until
// a batch comes back short or ctx ends.
func purge(ctx context.Context, target Target, before time.Time) (int64, error) {
	var total int64
	for {
		n, err := target.Delete(ctx, before, BatchSize)
		total += n
		if err != nil || n < BatchSize {
			return total, err
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}
