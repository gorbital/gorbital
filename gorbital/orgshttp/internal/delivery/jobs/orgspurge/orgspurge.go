// Package orgspurge runs the orgs_purge background job: it removes
// organisations deleted longer ago than the orgs.deleted_org_retention runtime
// setting, with every org-scoped row. Its schedule can be changed through
// /ops/jobs/definitions/orgs_purge.
package orgspurge

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

// Name identifies the job. It is public API: renaming it orphans its
// configuration overrides and history.
const Name = "orgs_purge"

// Args are the job's arguments.
type Args struct{}

// Kind returns [Name].
func (Args) Kind() string { return Name }

// Purge removes organisations past their retention and returns how many; the
// orgs use cases' Purge method matches it.
type Purge func(ctx context.Context) (int, error)

// Worker runs orgs_purge jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	purge  Purge
	logger *slog.Logger
}

// NewWorker returns a Worker.
func NewWorker(purge Purge, logger *slog.Logger) *Worker {
	return &Worker{purge: purge, logger: logger}
}

// Work runs one purge. An error retries the job.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	n, err := w.purge(ctx)
	if err != nil {
		return err
	}
	w.logger.InfoContext(ctx, "deleted organisations purged", "job", Name, "job_id", job.ID, "orgs", n)
	return nil
}
