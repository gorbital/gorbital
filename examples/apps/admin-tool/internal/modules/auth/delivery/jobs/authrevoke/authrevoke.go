// Package authrevoke runs the auth_revoke_tokens background job: it revokes
// the Google and Apple refresh tokens queued when an identity is unlinked or
// an account deleted, retrying failures with backoff (ADR-0046). Its
// schedule can be changed through /ops/jobs/definitions/auth_revoke_tokens.
package authrevoke

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"

	authdomain "example.com/admin-tool/internal/modules/auth/domain"
)

// Name identifies the job. It is public API: renaming it orphans its
// configuration overrides and history.
const Name = "auth_revoke_tokens"

// Args are the job's arguments.
type Args struct{}

// Kind returns [Name].
func (Args) Kind() string { return Name }

// Revoke revokes the provider tokens that are due; the auth use cases'
// RevokeProviderTokens method matches it.
type Revoke func(ctx context.Context) (authdomain.RevocationResult, error)

// Worker runs auth_revoke_tokens jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	revoke Revoke
	logger *slog.Logger
}

// NewWorker returns a Worker.
func NewWorker(revoke Revoke, logger *slog.Logger) *Worker {
	return &Worker{revoke: revoke, logger: logger}
}

// Work runs one batch. Only database errors retry the job: a provider's
// refusal is retried later by the revocation's own backoff.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	res, err := w.revoke(ctx)
	if err != nil {
		return err
	}
	if res != (authdomain.RevocationResult{}) {
		w.logger.InfoContext(ctx, "provider tokens revoked", "job", Name, "job_id", job.ID,
			"revoked", res.Revoked, "retrying", res.Retrying, "abandoned", res.Abandoned)
	}
	return nil
}
