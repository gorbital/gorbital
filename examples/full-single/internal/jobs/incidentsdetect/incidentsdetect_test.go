package incidentsdetect_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/observability"

	"example.com/acme-api/internal/jobs/incidentsdetect"
)

func TestWorker(t *testing.T) {
	job := &river.Job[incidentsdetect.Args]{JobRow: &rivertype.JobRow{ID: 9, Attempt: 1}}
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
		var (
			logs   bytes.Buffer
			events []audit.Event
		)
		res := observability.DetectionResult{Requests: 200, ServerErrors: 40, Action: tt.action}
		if tt.action != observability.DetectionNone {
			res.Incident, res.Update = inc, &observability.IncidentUpdate{ID: 11, Kind: observability.UpdateOpened}
		}
		w := incidentsdetect.NewWorker(func(context.Context) (observability.DetectionResult, error) { return res, nil },
			audit.RecorderFunc(func(_ context.Context, e audit.Event) error { events = append(events, e); return nil }),
			slog.New(slog.NewJSONHandler(&logs, nil)))
		if err := w.Work(context.Background(), job); err != nil {
			t.Fatalf("%q: Work() error = %v", tt.action, err)
		}
		if tt.wantAction == "" {
			if len(events) != 0 || logs.Len() != 0 {
				t.Errorf("no change: events %v, logs %s; want none", events, logs.String())
			}
			continue
		}
		if len(events) != 1 || events[0].Action != tt.wantAction || events[0].ActorKind != actor.KindSystem || events[0].ActorID != incidentsdetect.Name ||
			events[0].ResourceID != "4" || events[0].Metadata["detection"] != string(tt.action) {
			t.Errorf("%q: events = %+v", tt.action, events)
		}
		if !strings.Contains(logs.String(), tt.wantLog) || !strings.Contains(logs.String(), `"incident_id":4`) || !strings.Contains(logs.String(), `"error_rate":0.2`) {
			t.Errorf("%q: logs = %s", tt.action, logs.String())
		}
	}

	failing := incidentsdetect.NewWorker(func(context.Context) (observability.DetectionResult, error) {
		return observability.DetectionResult{}, errors.New("database unavailable")
	}, audit.RecorderFunc(func(context.Context, audit.Event) error { return nil }), slog.New(slog.DiscardHandler))
	if err := failing.Work(context.Background(), job); err == nil {
		t.Error("Work() with a failing detection error = nil")
	}
	if (incidentsdetect.Args{}).Kind() != incidentsdetect.Name {
		t.Error("Args.Kind() doesn't return Name")
	}
}
