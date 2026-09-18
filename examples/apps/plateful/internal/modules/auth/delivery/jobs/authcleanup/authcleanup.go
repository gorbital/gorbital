// Package authcleanup runs the auth_cleanup background job: it removes ended
// sessions, old email codes, accounts deleted longer ago than the
// auth.deleted_account_retention runtime setting, accounts still
// unverified after auth.unverified_account_ttl, and API keys expired or
// revoked more than 30 days ago, recording each key's expiry first. Its schedule can be changed
// through /ops/jobs/definitions/auth_cleanup.
package authcleanup

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"

	authdomain "example.com/plateful/internal/modules/auth/domain"
)

// Name identifies the job. It is public API: renaming it orphans its
// configuration overrides and history.
const Name = "auth_cleanup"

// Args are the job's arguments.
type Args struct{}

// Kind returns [Name].
func (Args) Kind() string { return Name }

// Cleanup removes expired authentication data; the auth use cases' Cleanup
// method matches it.
type Cleanup func(ctx context.Context) (authdomain.CleanupResult, error)

// Worker runs auth_cleanup jobs.
type Worker struct {
	river.WorkerDefaults[Args]
	cleanup Cleanup
	logger  *slog.Logger
}

// NewWorker returns a Worker.
func NewWorker(cleanup Cleanup, logger *slog.Logger) *Worker {
	return &Worker{cleanup: cleanup, logger: logger}
}

// Work runs one cleanup. An error retries the job.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	res, err := w.cleanup(ctx)
	if err != nil {
		return err
	}
	w.logger.InfoContext(ctx, "authentication data cleaned up", "job", Name, "job_id", job.ID,
		"sessions", res.Sessions, "codes", res.Codes, "challenges", res.Challenges, "accounts", res.Users, "unverified_accounts", res.Unverified,
		"expired_api_keys", res.ExpiredAPIKeys, "api_keys", res.APIKeys)
	return nil
}
