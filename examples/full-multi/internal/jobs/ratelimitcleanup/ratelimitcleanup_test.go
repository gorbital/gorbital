package ratelimitcleanup_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"example.com/acme-api/internal/jobs/ratelimitcleanup"
)

func TestWorker(t *testing.T) {
	var logs bytes.Buffer
	batches := []int64{ratelimitcleanup.BatchSize, ratelimitcleanup.BatchSize, 42}
	calls := 0
	w := ratelimitcleanup.NewWorker(func(_ context.Context, limit int) (int64, error) {
		if limit != ratelimitcleanup.BatchSize {
			t.Errorf("limit = %d, want %d", limit, ratelimitcleanup.BatchSize)
		}
		n := batches[calls]
		calls++
		return n, nil
	}, slog.New(slog.NewJSONHandler(&logs, nil)))

	job := &river.Job[ratelimitcleanup.Args]{JobRow: &rivertype.JobRow{ID: 3, Attempt: 1}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if calls != 3 || !strings.Contains(logs.String(), `"buckets":20042`) {
		t.Errorf("calls = %d, logs = %s; want 3 batches and the total", calls, logs.String())
	}

	failing := ratelimitcleanup.NewWorker(func(context.Context, int) (int64, error) {
		return 0, errors.New("database unavailable")
	}, slog.New(slog.DiscardHandler))
	if err := failing.Work(context.Background(), job); err == nil {
		t.Error("Work() with a failing delete error = nil, want an error to retry")
	}
	if (ratelimitcleanup.Args{}).Kind() != ratelimitcleanup.Name {
		t.Error("Args.Kind() doesn't return Name")
	}
}
