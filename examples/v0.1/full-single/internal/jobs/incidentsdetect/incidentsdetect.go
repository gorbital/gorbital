// Package incidentsdetect runs the incidents_detect background job: every
// minute it compares the error rate of every instance's requests with
// incidents.error_rate_threshold, opens an automatic incident when it is
// above, and notes on that incident when it recovers or rises again
// (ADR-0064). Operators resolve automatic incidents; nothing is paged, but
// each change is logged, counted in the incidents.detections metric and
// recorded in the audit log. Its schedule can be changed through
// /ops/jobs/definitions/incidents_detect.
package incidentsdetect

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/riverqueue/river"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/observability"
)

// Name identifies the job. It is public API: renaming it orphans its
// configuration overrides and history.
const Name = "incidents_detect"

// Args are the job's arguments.
type Args struct{}

// Kind returns [Name].
func (Args) Kind() string { return Name }

// Detect runs one detection with the current settings; a closure over
// observability.Store's DetectIncident matches it.
type Detect func(ctx context.Context) (observability.DetectionResult, error)

// Worker runs incidents_detect jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	detect   Detect
	recorder audit.Recorder
	logger   *slog.Logger
}

// NewWorker returns a Worker.
func NewWorker(detect Detect, recorder audit.Recorder, logger *slog.Logger) *Worker {
	return &Worker{detect: detect, recorder: recorder, logger: logger}
}

// Work runs a detection and reports what it changed. An error fails the run;
// the next minute's run detects again.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	res, err := w.detect(ctx)
	if err != nil {
		return err
	}
	if res.Action == observability.DetectionNone || res.Incident == nil {
		return nil
	}
	attrs := []any{
		"job", Name, "job_id", job.ID, "incident_id", res.Incident.ID, "error_rate", res.ErrorRate(),
		"requests", res.Requests, "server_errors", res.ServerErrors,
	}
	event := audit.Event{
		ActorKind: actor.KindSystem, ActorID: Name, ActorLabel: Name,
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
		w.logger.ErrorContext(ctx, "record incident detection", "job", Name, "incident_id", res.Incident.ID, "err", err)
	}
	return nil
}
