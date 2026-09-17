// Package builtinjobs holds the background jobs gorbital.New defines for
// what it builds: expired rate-limit buckets and idempotency keys, old
// request minutes, automatic incidents and data retention. They are the
// jobs of a v0.1 app's internal/jobs, with the same names, schedules and
// behaviour, so their configuration overrides and history in /ops/jobs
// carry over (ADR-0033, ADR-0051). Job names are public API.
package builtinjobs

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/riverqueue/river"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/observability"
)

// Job names. Renaming one orphans its configuration overrides and history.
const (
	RateLimitCleanup     = "ratelimit_cleanup"
	IdempotencyCleanup   = "idempotency_cleanup"
	ObservabilityCleanup = "observability_cleanup"
	IncidentsDetect      = "incidents_detect"
	Retention            = "retention"
)

// Batch sizes: how many rows one delete statement removes.
const (
	RateLimitBatch     = 10_000
	IdempotencyBatch   = 1_000
	ObservabilityBatch = 5_000
	RetentionBatch     = 5_000
)

// Deps are what the built-in jobs use. Every function is called only when
// a job runs.
type Deps struct {
	Logger   *slog.Logger
	Recorder audit.Recorder
	// RateLimitCleanup deletes up to limit expired rate-limit buckets
	// (ratelimitpg.Store.DeleteExpired).
	RateLimitCleanup func(ctx context.Context, limit int) (int64, error)
	// IdempotencyCleanup deletes up to limit expired idempotency keys
	// (idempotency.Store.DeleteExpired).
	IdempotencyCleanup func(ctx context.Context, limit int) (int64, error)
	// ObservabilityCleanup deletes up to limit request minutes older than
	// before, kept for ObservabilityRetention.
	ObservabilityCleanup   func(ctx context.Context, before time.Time, limit int) (int64, error)
	ObservabilityRetention func(ctx context.Context) time.Duration
	// DetectIncidents runs one incident detection with the current settings.
	DetectIncidents func(ctx context.Context) (observability.DetectionResult, error)
	// RetentionTargets are the data the retention job deletes.
	RetentionTargets []Target
}

// Define declares the built-in jobs with their code defaults; operators
// override them in /ops/jobs.
func Define(defs *jobs.Definitions, d Deps) {
	jobs.Define(defs, jobs.Definition[RateLimitCleanupArgs]{
		Name:        RateLimitCleanup,
		Description: "Deletes shared rate limit buckets whose keys are back to a full budget.",
		Worker:      &rateLimitCleanupWorker{deleteExpired: d.RateLimitCleanup, logger: d.Logger},
		NewArgs:     func() RateLimitCleanupArgs { return RateLimitCleanupArgs{} },
		Enabled:     true, Schedule: "@every 1h", Timeout: 5 * time.Minute, MaxAttempts: 3, Queue: "default", Priority: 3,
	})
	jobs.Define(defs, jobs.Definition[IdempotencyCleanupArgs]{
		Name:        IdempotencyCleanup,
		Description: "Deletes idempotency keys and their stored responses once they are older than idempotency.retention.",
		Worker:      &idempotencyCleanupWorker{deleteExpired: d.IdempotencyCleanup, logger: d.Logger},
		NewArgs:     func() IdempotencyCleanupArgs { return IdempotencyCleanupArgs{} },
		Enabled:     true, Schedule: "@every 1h", Timeout: 5 * time.Minute, MaxAttempts: 3, Queue: "default", Priority: 3,
	})
	jobs.Define(defs, jobs.Definition[ObservabilityCleanupArgs]{
		Name:        ObservabilityCleanup,
		Description: "Deletes request minutes, which /ops/observability reads, once they are older than observability.retention.",
		Worker:      &observabilityCleanupWorker{deleteBefore: d.ObservabilityCleanup, retention: d.ObservabilityRetention, logger: d.Logger, now: time.Now},
		NewArgs:     func() ObservabilityCleanupArgs { return ObservabilityCleanupArgs{} },
		Enabled:     true, Schedule: "@every 1h", Timeout: 5 * time.Minute, MaxAttempts: 3, Queue: "default", Priority: 3,
	})
	jobs.Define(defs, jobs.Definition[IncidentsDetectArgs]{
		Name:        IncidentsDetect,
		Description: "Opens an automatic incident when the server error rate over incidents.detection_window is above incidents.error_rate_threshold, and notes when it recovers.",
		Worker:      &incidentsDetectWorker{detect: d.DetectIncidents, recorder: d.Recorder, logger: d.Logger},
		NewArgs:     func() IncidentsDetectArgs { return IncidentsDetectArgs{} },
		Enabled:     true, Schedule: "@every 1m", Timeout: 30 * time.Second,
		MaxAttempts: 1, // the next minute's run detects again
		Queue:       "default", Priority: 2,
	})
	jobs.Define(defs, jobs.Definition[RetentionArgs]{
		Name:        Retention,
		Description: "Deletes audit events older than audit.retention and setting and job configuration history older than ops.history_retention.",
		Worker:      NewRetentionWorker(d.RetentionTargets, d.Recorder, d.Logger),
		NewArgs:     func() RetentionArgs { return RetentionArgs{} },
		Enabled:     true, Schedule: "15 4 * * *", Timeout: 30 * time.Minute, MaxAttempts: 3, Queue: "default", Priority: 3,
	})
}

// RateLimitCleanupArgs are the ratelimit_cleanup job's arguments.
type RateLimitCleanupArgs struct{}

// Kind returns RateLimitCleanup.
func (RateLimitCleanupArgs) Kind() string { return RateLimitCleanup }

type rateLimitCleanupWorker struct {
	river.WorkerDefaults[RateLimitCleanupArgs]
	deleteExpired func(ctx context.Context, limit int) (int64, error)
	logger        *slog.Logger
}

// Work deletes expired buckets in batches until none are left.
func (w *rateLimitCleanupWorker) Work(ctx context.Context, job *river.Job[RateLimitCleanupArgs]) error {
	total, err := deleteBatches(ctx, RateLimitBatch, w.deleteExpired)
	if total > 0 {
		w.logger.InfoContext(ctx, "expired rate limit buckets deleted", "job", RateLimitCleanup, "job_id", job.ID, "buckets", total)
	}
	return err
}

// IdempotencyCleanupArgs are the idempotency_cleanup job's arguments.
type IdempotencyCleanupArgs struct{}

// Kind returns IdempotencyCleanup.
func (IdempotencyCleanupArgs) Kind() string { return IdempotencyCleanup }

type idempotencyCleanupWorker struct {
	river.WorkerDefaults[IdempotencyCleanupArgs]
	deleteExpired func(ctx context.Context, limit int) (int64, error)
	logger        *slog.Logger
}

// Work deletes expired keys in batches until none are left.
func (w *idempotencyCleanupWorker) Work(ctx context.Context, job *river.Job[IdempotencyCleanupArgs]) error {
	total, err := deleteBatches(ctx, IdempotencyBatch, w.deleteExpired)
	if total > 0 {
		w.logger.InfoContext(ctx, "expired idempotency keys deleted", "job", IdempotencyCleanup, "job_id", job.ID, "keys", total)
	}
	return err
}

// deleteBatches calls del until a batch comes back short, returning the
// total deleted.
func deleteBatches(ctx context.Context, batch int, del func(ctx context.Context, limit int) (int64, error)) (int64, error) {
	var total int64
	for {
		n, err := del(ctx, batch)
		total += n
		if err != nil || n < int64(batch) {
			return total, err
		}
	}
}

// ObservabilityCleanupArgs are the observability_cleanup job's arguments.
type ObservabilityCleanupArgs struct{}

// Kind returns ObservabilityCleanup.
func (ObservabilityCleanupArgs) Kind() string { return ObservabilityCleanup }

type observabilityCleanupWorker struct {
	river.WorkerDefaults[ObservabilityCleanupArgs]
	deleteBefore func(ctx context.Context, before time.Time, limit int) (int64, error)
	retention    func(context.Context) time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

// Work deletes old minutes in batches until none are left.
func (w *observabilityCleanupWorker) Work(ctx context.Context, job *river.Job[ObservabilityCleanupArgs]) error {
	before := w.now().Add(-w.retention(ctx))
	total, err := deleteBatches(ctx, ObservabilityBatch, func(ctx context.Context, limit int) (int64, error) {
		return w.deleteBefore(ctx, before, limit)
	})
	if total > 0 {
		w.logger.InfoContext(ctx, "old request minutes deleted", "job", ObservabilityCleanup, "job_id", job.ID, "minutes", total, "before", before)
	}
	return err
}

// IncidentsDetectArgs are the incidents_detect job's arguments.
type IncidentsDetectArgs struct{}

// Kind returns IncidentsDetect.
func (IncidentsDetectArgs) Kind() string { return IncidentsDetect }

type incidentsDetectWorker struct {
	river.WorkerDefaults[IncidentsDetectArgs]
	detect   func(ctx context.Context) (observability.DetectionResult, error)
	recorder audit.Recorder
	logger   *slog.Logger
}

// Work runs a detection and reports what it changed. An error fails the
// run; the next minute's run detects again.
func (w *incidentsDetectWorker) Work(ctx context.Context, job *river.Job[IncidentsDetectArgs]) error {
	res, err := w.detect(ctx)
	if err != nil {
		return err
	}
	if res.Action == observability.DetectionNone || res.Incident == nil {
		return nil
	}
	attrs := []any{
		"job", IncidentsDetect, "job_id", job.ID, "incident_id", res.Incident.ID, "error_rate", res.ErrorRate(),
		"requests", res.Requests, "server_errors", res.ServerErrors,
	}
	event := audit.Event{
		ActorKind: actor.KindSystem, ActorID: IncidentsDetect, ActorLabel: IncidentsDetect,
		ResourceType: "incident", ResourceID: strconv.FormatInt(res.Incident.ID, 10), Outcome: audit.OutcomeSuccess,
		Metadata: map[string]any{
			"source": string(res.Incident.Source), "status": string(res.Incident.Status), "severity": string(res.Incident.Severity),
			"detection": string(res.Action), "requests": res.Requests, "server_errors": res.ServerErrors,
		},
	}
	if res.Update != nil {
		event.Metadata["update_id"], event.Metadata["update_kind"] = res.Update.ID, string(res.Update.Kind)
	}
	switch res.Action {
	case observability.DetectionOpened:
		w.logger.WarnContext(ctx, "incident opened: error rate above threshold", attrs...)
		event.Action = "ops.incident.opened"
	case observability.DetectionBreaching:
		w.logger.WarnContext(ctx, "incident error rate above threshold again", attrs...)
		event.Action = "ops.incident.updated"
	default:
		w.logger.InfoContext(ctx, "incident error rate recovered", attrs...)
		event.Action = "ops.incident.updated"
	}
	// The incident changed already; a failed audit write is logged, not
	// retried, like other automatic changes.
	if err := w.recorder.Record(ctx, event); err != nil {
		w.logger.ErrorContext(ctx, "record incident detection", "job", IncidentsDetect, "incident_id", res.Incident.ID, "err", err)
	}
	return nil
}

// RetentionArgs are the retention job's arguments.
type RetentionArgs struct{}

// Kind returns Retention.
func (RetentionArgs) Kind() string { return Retention }

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

// RetentionWorker runs retention jobs.
type RetentionWorker struct {
	river.WorkerDefaults[RetentionArgs]
	targets  []Target
	recorder audit.Recorder
	logger   *slog.Logger
	now      func() time.Time
}

// NewRetentionWorker returns a worker for targets.
func NewRetentionWorker(targets []Target, recorder audit.Recorder, logger *slog.Logger) *RetentionWorker {
	return &RetentionWorker{targets: targets, recorder: recorder, logger: logger, now: time.Now}
}

// Work deletes each target's expired rows. A failing target doesn't stop the
// others; the job fails, and retries, when any did.
func (w *RetentionWorker) Work(ctx context.Context, job *river.Job[RetentionArgs]) error {
	ctx = actor.With(ctx, actor.System(Retention))
	var errs []error
	for _, target := range w.targets {
		before := w.now().Add(-target.Retention(ctx)).UTC()
		deleted, err := purge(ctx, target, before)
		if deleted > 0 {
			w.logger.InfoContext(ctx, "expired data deleted", "job", Retention, "job_id", job.ID, "data", target.Name, "rows", deleted, "before", before)
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
		n, err := target.Delete(ctx, before, RetentionBatch)
		total += n
		if err != nil || n < RetentionBatch {
			return total, err
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}
