package app

import (
	"time"

	"apistock.dev/modules/jobs"

	"example.com/acme-api/internal/jobs/retention"
)

// defineRetentionJob declares the retention job with its code defaults.
// Operators can override them in /ops/jobs/definitions/retention; the
// retention periods themselves are runtime settings (ADR-0051).
func defineRetentionJob(defs *jobs.Definitions, deps jobDeps) {
	jobs.Define(defs, jobs.Definition[retention.Args]{
		Name:        retention.Name,
		Description: "Deletes audit events older than audit.retention and setting and job configuration history older than ops.history_retention.",
		Worker:      retention.NewWorker(deps.retentionTargets, deps.recorder, deps.logger),
		NewArgs:     func() retention.Args { return retention.Args{} },
		Enabled:     true,
		Schedule:    "15 4 * * *",
		Timeout:     30 * time.Minute,
		MaxAttempts: 3,
		Queue:       "default",
		Priority:    3,
	})
}
