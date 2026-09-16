package jobs

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/mail"
)

// errorHandler logs each failed attempt once, at the edge (ADR-0018):
// a warning while retries remain, an error on the last attempt. Email
// addresses in error messages are redacted: logs carry IDs, not addresses.
type errorHandler struct {
	logger *slog.Logger
}

var _ river.ErrorHandler = (*errorHandler)(nil)

func (h *errorHandler) HandleError(ctx context.Context, job *rivertype.JobRow, err error) *river.ErrorHandlerResult {
	h.logger.Log(ctx, level(job), "job failed", jobAttrs(job, slog.String("err", mail.RedactAddresses(err.Error())))...)
	return nil
}

func (h *errorHandler) HandlePanic(ctx context.Context, job *rivertype.JobRow, panicVal any, trace string) *river.ErrorHandlerResult {
	h.logger.Log(ctx, level(job), "job panicked", jobAttrs(job,
		slog.String("panic", mail.RedactAddresses(fmt.Sprint(panicVal))),
		slog.String("stack", trace),
	)...)
	return nil
}

func level(job *rivertype.JobRow) slog.Level {
	if job.Attempt >= job.MaxAttempts {
		return slog.LevelError
	}
	return slog.LevelWarn
}

func jobAttrs(job *rivertype.JobRow, extra ...slog.Attr) []any {
	attrs := []any{
		slog.Int64("job_id", job.ID),
		slog.String("job_kind", job.Kind),
		slog.String("queue", job.Queue),
		slog.Int("attempt", job.Attempt),
		slog.Int("max_attempts", job.MaxAttempts),
	}
	for _, a := range extra {
		attrs = append(attrs, a)
	}
	return attrs
}
