package heartbeat_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"example.com/acme-api/internal/jobs/heartbeat"
)

func TestWorker(t *testing.T) {
	var logs bytes.Buffer
	w := heartbeat.NewWorker(slog.New(slog.NewJSONHandler(&logs, nil)))

	job := &river.Job[heartbeat.Args]{JobRow: &rivertype.JobRow{ID: 7, Attempt: 1}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if !strings.Contains(logs.String(), `"job_id":7`) {
		t.Errorf("logs = %s, want a line with the job ID", logs.String())
	}
	if (heartbeat.Args{}).Kind() != heartbeat.Name {
		t.Error("Args.Kind() doesn't return Name")
	}
}
