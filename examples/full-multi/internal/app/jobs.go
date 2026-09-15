package app

import (
	"log/slog"

	"gorbital.dev/audit"
	"gorbital.dev/modules/jobs"

	"example.com/acme-api/internal/jobs/authcleanup"
	"example.com/acme-api/internal/jobs/authrevoke"
	"example.com/acme-api/internal/jobs/orgspurge"
	"example.com/acme-api/internal/jobs/ratelimitcleanup"
	"example.com/acme-api/internal/jobs/retention"
)

// jobDeps are what job workers may use. Add stores and clients here when a
// job needs them.
type jobDeps struct {
	logger           *slog.Logger
	recorder         audit.Recorder
	authCleanup      authcleanup.Cleanup
	authRevokeTokens authrevoke.Revoke
	rateLimitCleanup ratelimitcleanup.DeleteExpired
	orgsPurge        orgspurge.Purge
	// retentionTargets are the data the retention job deletes (ADR-0051).
	retentionTargets []retention.Target
}

// defineJobs declares every background job. Each job's schedule, timeout and
// retries can be changed at runtime through /ops/jobs (ADR-0033).
// `orb gen job` adds a line at the anchor.
func defineJobs(defs *jobs.Definitions, deps jobDeps) {
	//orb:anchor jobs
	defineHeartbeatJob(defs, deps)
	defineAuthCleanupJob(defs, deps)
	defineAuthRevokeTokensJob(defs, deps)
	defineOrgsPurgeJob(defs, deps)
	defineRateLimitCleanupJob(defs, deps)
	defineRetentionJob(defs, deps)
}
