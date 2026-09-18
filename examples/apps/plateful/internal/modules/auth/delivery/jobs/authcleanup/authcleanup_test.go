package authcleanup_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"example.com/plateful/internal/modules/auth/delivery/jobs/authcleanup"
	authdomain "example.com/plateful/internal/modules/auth/domain"
)

func TestWorker(t *testing.T) {
	var logs bytes.Buffer
	w := authcleanup.NewWorker(func(context.Context) (authdomain.CleanupResult, error) {
		return authdomain.CleanupResult{Sessions: 4, Codes: 2, Users: 1}, nil
	}, slog.New(slog.NewJSONHandler(&logs, nil)))

	job := &river.Job[authcleanup.Args]{JobRow: &rivertype.JobRow{ID: 9, Attempt: 1}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if !strings.Contains(logs.String(), `"sessions":4`) || !strings.Contains(logs.String(), `"accounts":1`) {
		t.Errorf("logs = %s, want the counts", logs.String())
	}

	failing := authcleanup.NewWorker(func(context.Context) (authdomain.CleanupResult, error) {
		return authdomain.CleanupResult{}, errors.New("database unavailable")
	}, slog.New(slog.DiscardHandler))
	if err := failing.Work(context.Background(), job); err == nil {
		t.Error("Work() with a failing cleanup error = nil, want an error to retry")
	}
	if (authcleanup.Args{}).Kind() != authcleanup.Name {
		t.Error("Args.Kind() doesn't return Name")
	}
}
