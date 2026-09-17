package builtinjobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/observability"
)

func TestDefine(t *testing.T) {
	Define(jobs.NewDefinitions(), Deps{})
	for kind, want := range map[string]string{
		RateLimitCleanupArgs{}.Kind(): "ratelimit_cleanup", IdempotencyCleanupArgs{}.Kind(): "idempotency_cleanup",
		ObservabilityCleanupArgs{}.Kind(): "observability_cleanup", IncidentsDetectArgs{}.Kind(): "incidents_detect", RetentionArgs{}.Kind(): "retention",
	} {
		if kind != want {
			t.Errorf("job kind %q, want %q: job names are public API", kind, want)
		}
	}
}

// batches deletes rows in batches of limit, counting calls.
type batches struct {
	rows, calls int
	fail        bool
}

func (b *batches) delete(_ context.Context, limit int) (int64, error) {
	b.calls++
	if b.fail {
		return 0, errors.New("database unavailable")
	}
	n := min(b.rows, limit)
	b.rows -= n
	return int64(n), nil
}

func TestCleanupWorkersDeleteInBatches(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	buckets := &batches{rows: RateLimitBatch*2 + 3}
	if err := (&rateLimitCleanupWorker{deleteExpired: buckets.delete, logger: logger}).Work(context.Background(), &river.Job[RateLimitCleanupArgs]{JobRow: &rivertype.JobRow{ID: 1}}); err != nil {
		t.Fatal(err)
	}
	if buckets.rows != 0 || buckets.calls != 3 || !strings.Contains(logs.String(), "buckets=20003") {
		t.Errorf("ratelimit_cleanup left %d after %d calls; logs %s", buckets.rows, buckets.calls, logs.String())
	}
	keys := &batches{fail: true}
	if err := (&idempotencyCleanupWorker{deleteExpired: keys.delete, logger: logger}).Work(context.Background(), &river.Job[IdempotencyCleanupArgs]{JobRow: &rivertype.JobRow{ID: 2}}); err == nil {
		t.Error("idempotency_cleanup with a failing store error = nil, want the job to retry")
	}

	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	var before time.Time
	minutes := &batches{rows: 7}
	w := &observabilityCleanupWorker{
		deleteBefore: func(ctx context.Context, b time.Time, limit int) (int64, error) {
			before = b
			return minutes.delete(ctx, limit)
		},
		retention: func(context.Context) time.Duration { return 24 * time.Hour },
		logger:    logger, now: func() time.Time { return now },
	}
	if err := w.Work(context.Background(), &river.Job[ObservabilityCleanupArgs]{JobRow: &rivertype.JobRow{ID: 3}}); err != nil {
		t.Fatal(err)
	}
	if !before.Equal(now.Add(-24*time.Hour)) || minutes.rows != 0 {
		t.Errorf("observability_cleanup deleted before %v, left %d", before, minutes.rows)
	}
}

type recorder struct{ events []audit.Event }

func (r *recorder) Record(ctx context.Context, e audit.Event) error {
	if a, _ := actor.From(ctx); a.ID != Retention {
		return errors.New("recorded without the retention system actor")
	}
	r.events = append(r.events, e)
	return nil
}

func TestRetentionWorker(t *testing.T) {
	events := &batches{rows: RetentionBatch*2 + 10}
	history := &batches{fail: true}
	days := func(d int) func(context.Context) time.Duration {
		return func(context.Context) time.Duration { return time.Duration(d) * 24 * time.Hour }
	}
	rec := &recorder{}
	w := NewRetentionWorker([]Target{
		{Name: "settings_history", Retention: days(365), Delete: func(ctx context.Context, _ time.Time, limit int) (int64, error) { return history.delete(ctx, limit) }},
		{Name: "audit_events", Retention: days(365), Delete: func(ctx context.Context, _ time.Time, limit int) (int64, error) { return events.delete(ctx, limit) }},
	}, rec, slog.New(slog.DiscardHandler))
	err := w.Work(context.Background(), &river.Job[RetentionArgs]{JobRow: &rivertype.JobRow{ID: 3}})
	if err == nil {
		t.Error("Work() with a failing target error = nil, want the job to retry")
	}
	if events.rows != 0 || events.calls != 3 {
		t.Errorf("a failing target stopped the others: %d rows left after %d deletes", events.rows, events.calls)
	}
	if len(rec.events) != 1 || rec.events[0].Action != "retention.purged" || rec.events[0].ResourceID != "audit_events" || rec.events[0].Metadata["rows"] != int64(RetentionBatch*2+10) {
		t.Errorf("audit events = %+v, want one retention.purged for audit_events with the count", rec.events)
	}
}

func TestIncidentsDetectWorker(t *testing.T) {
	job := &river.Job[IncidentsDetectArgs]{JobRow: &rivertype.JobRow{ID: 9, Attempt: 1}}
	inc := &observability.Incident{ID: 4, Source: observability.SourceAutomatic, Status: observability.StatusInvestigating, Severity: observability.SeveritySev2}
	for _, tt := range []struct {
		action     observability.DetectionAction
		wantAction string
		wantLog    string
	}{
		{observability.DetectionNone, "", ""},
		{observability.DetectionOpened, "ops.incident.opened", `"level":"WARN","msg":"incident opened: error rate above threshold"`},
		{observability.DetectionRecovered, "ops.incident.updated", `"msg":"incident error rate recovered"`},
		{observability.DetectionBreaching, "ops.incident.updated", `"msg":"incident error rate above threshold again"`},
	} {
		var logs bytes.Buffer
		var events []audit.Event
		res := observability.DetectionResult{Requests: 200, ServerErrors: 40, Action: tt.action}
		if tt.action != observability.DetectionNone {
			res.Incident, res.Update = inc, &observability.IncidentUpdate{ID: 11, Kind: observability.UpdateOpened}
		}
		w := &incidentsDetectWorker{
			detect:   func(context.Context) (observability.DetectionResult, error) { return res, nil },
			recorder: audit.RecorderFunc(func(_ context.Context, e audit.Event) error { events = append(events, e); return nil }),
			logger:   slog.New(slog.NewJSONHandler(&logs, nil)),
		}
		if err := w.Work(context.Background(), job); err != nil {
			t.Fatalf("%q: Work() error = %v", tt.action, err)
		}
		if tt.wantAction == "" {
			if len(events) != 0 || logs.Len() != 0 {
				t.Errorf("no change: events %v, logs %s; want none", events, logs.String())
			}
			continue
		}
		if len(events) != 1 || events[0].Action != tt.wantAction || events[0].ActorID != IncidentsDetect || events[0].ResourceID != "4" {
			t.Errorf("%q: events = %+v", tt.action, events)
		}
		if !strings.Contains(logs.String(), tt.wantLog) || !strings.Contains(logs.String(), `"error_rate":0.2`) {
			t.Errorf("%q: logs = %s", tt.action, logs.String())
		}
	}
}
