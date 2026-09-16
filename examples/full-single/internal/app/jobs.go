package app

import (
	"context"
	"log/slog"
	"time"

	"gorbital.dev/audit"
	"gorbital.dev/modules/jobs"

	"example.com/acme-api/internal/jobs/authcleanup"
	"example.com/acme-api/internal/jobs/authrevoke"
	"example.com/acme-api/internal/jobs/idempotencycleanup"
	"example.com/acme-api/internal/jobs/incidentsdetect"
	"example.com/acme-api/internal/jobs/observabilitycleanup"
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
	// idempotencyCleanup deletes expired idempotency keys (ADR-0060).
	idempotencyCleanup idempotencycleanup.DeleteExpired
	// observabilityCleanup deletes request minutes older than
	// observabilityRetention, and detectIncidents opens automatic incidents
	// (ADR-0064).
	observabilityCleanup   observabilitycleanup.DeleteBefore
	observabilityRetention func(context.Context) time.Duration
	detectIncidents        incidentsdetect.Detect
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
	defineRateLimitCleanupJob(defs, deps)
	defineIdempotencyCleanupJob(defs, deps)
	defineObservabilityCleanupJob(defs, deps)
	defineIncidentsDetectJob(defs, deps)
	defineRetentionJob(defs, deps)
}
