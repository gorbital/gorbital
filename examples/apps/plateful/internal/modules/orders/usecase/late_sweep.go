package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// docs:start late-sweep-args

// LateSweepJob is the name of the job that looks for orders a kitchen is
// sitting on. Job names are public API: renaming one orphans its
// configuration overrides in /ops/jobs and its history.
const LateSweepJob = "orders_late_sweep"

// LateSweepArgs are the job's arguments. It takes none: it reads the whole
// platform, and the time it compares against is the moment it runs.
type LateSweepArgs struct{}

// Kind returns [LateSweepJob].
func (LateSweepArgs) Kind() string { return LateSweepJob }

// docs:end late-sweep-args

// A Notifier tells one restaurant something. The notifications module
// implements it (notifications.FromWorker); the orders module never learns
// how a notification is delivered, only that it asked for one.
type Notifier func(ctx context.Context, orgID, title string, lines []string) error

// A LateSweepWorker runs the orders_late_sweep job. module.go defines the
// job with it.
type LateSweepWorker struct {
	river.WorkerDefaults[LateSweepArgs]
	svc    *Service
	notify Notifier
	logger *slog.Logger
	now    func() time.Time
}

// NewLateSweepWorker returns a worker that reports svc's late orders through
// notify.
func NewLateSweepWorker(svc *Service, notify Notifier, logger *slog.Logger) *LateSweepWorker {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &LateSweepWorker{svc: svc, notify: notify, logger: logger, now: time.Now}
}

// docs:start late-sweep-work

// Work finds every restaurant with late orders and asks for one notification
// each.
//
// How long is "late" is a runtime setting read when the job runs, so an
// operator changes it in /ops/settings and the next sweep, five minutes
// later, uses the new value — no deploy, and no restart.
//
// A notification that can't be enqueued fails the run, which the job system
// retries. The ones already enqueued are separate jobs with attempts of
// their own, so the retry doesn't send them twice.
func (w *LateSweepWorker) Work(ctx context.Context, job *river.Job[LateSweepArgs]) error {
	now := w.now().UTC()
	reports, err := w.svc.LateOrders(ctx, now, LateSweepLimit)
	if err != nil {
		return err
	}
	for _, report := range reports {
		if err := w.notify(ctx, report.OrgID, report.Title(), report.Lines(now)); err != nil {
			return err
		}
		w.logger.InfoContext(ctx, "late orders reported",
			"job", LateSweepJob, "job_id", job.ID, "org_id", report.OrgID, "orders", len(report.Orders))
	}
	return nil
}

// docs:end late-sweep-work
