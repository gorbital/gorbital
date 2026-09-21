package orgspurge_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"example.com/shelfie/internal/modules/orgs/delivery/jobs/orgspurge"
)

func TestWorker(t *testing.T) {
	var logs bytes.Buffer
	w := orgspurge.NewWorker(func(context.Context) (int, error) { return 3, nil }, slog.New(slog.NewJSONHandler(&logs, nil)))
	job := &river.Job[orgspurge.Args]{JobRow: &rivertype.JobRow{ID: 7, Attempt: 1}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if !strings.Contains(logs.String(), `"orgs":3`) {
		t.Errorf("logs = %s, want the count", logs.String())
	}
	failing := orgspurge.NewWorker(func(context.Context) (int, error) { return 0, errors.New("database unavailable") }, slog.New(slog.DiscardHandler))
	if err := failing.Work(context.Background(), job); err == nil {
		t.Error("Work() with a failing purge error = nil, want an error to retry")
	}
	if (orgspurge.Args{}).Kind() != orgspurge.Name {
		t.Error("Args.Kind() doesn't return Name")
	}
}
