package app

import (
	"log/slog"

	"apistock.dev/audit"
	"apistock.dev/modules/jobs"

	"example.com/acme-api/internal/jobs/authcleanup"
	"example.com/acme-api/internal/jobs/orgspurge"
	"example.com/acme-api/internal/jobs/retention"
)

// jobDeps are what job workers may use. Add stores and clients here when a
// job needs them.
type jobDeps struct {
	logger      *slog.Logger
	recorder    audit.Recorder
	authCleanup authcleanup.Cleanup
	orgsPurge   orgspurge.Purge
	// retentionTargets are the data the retention job deletes (ADR-0051).
	retentionTargets []retention.Target
}

// defineJobs declares every background job. Each job's schedule, timeout and
// retries can be changed at runtime through /ops/jobs (ADR-0033).
// `aps gen job` adds a line at the anchor.
func defineJobs(defs *jobs.Definitions, deps jobDeps) {
	//aps:anchor jobs
	defineHeartbeatJob(defs, deps)
	defineAuthCleanupJob(defs, deps)
	defineOrgsPurgeJob(defs, deps)
	defineRetentionJob(defs, deps)
}
