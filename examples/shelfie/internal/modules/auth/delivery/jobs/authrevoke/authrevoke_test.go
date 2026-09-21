package authrevoke_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"example.com/shelfie/internal/modules/auth/delivery/jobs/authrevoke"
	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

func TestWorker(t *testing.T) {
	var logs bytes.Buffer
	w := authrevoke.NewWorker(func(context.Context) (authdomain.RevocationResult, error) {
		return authdomain.RevocationResult{Revoked: 3, Retrying: 1}, nil
	}, slog.New(slog.NewJSONHandler(&logs, nil)))

	job := &river.Job[authrevoke.Args]{JobRow: &rivertype.JobRow{ID: 7, Attempt: 1}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatalf("Work() error = %v", err)
	}
	if !strings.Contains(logs.String(), `"revoked":3`) || !strings.Contains(logs.String(), `"retrying":1`) {
		t.Errorf("logs = %s, want the counts", logs.String())
	}

	logs.Reset()
	idle := authrevoke.NewWorker(func(context.Context) (authdomain.RevocationResult, error) {
		return authdomain.RevocationResult{}, nil
	}, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err := idle.Work(context.Background(), job); err != nil || logs.Len() != 0 {
		t.Errorf("Work() with nothing due = %v, logs %q; want no error and no log line", err, logs.String())
	}

	failing := authrevoke.NewWorker(func(context.Context) (authdomain.RevocationResult, error) {
		return authdomain.RevocationResult{}, errors.New("database unavailable")
	}, slog.New(slog.DiscardHandler))
	if err := failing.Work(context.Background(), job); err == nil {
		t.Error("Work() with a database error = nil, want an error to retry")
	}
	if (authrevoke.Args{}).Kind() != authrevoke.Name {
		t.Error("Args.Kind() doesn't return Name")
	}
}
