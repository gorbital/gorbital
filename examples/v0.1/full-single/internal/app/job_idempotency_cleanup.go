package app

import (
	"time"

	"gorbital.dev/modules/jobs"

	"example.com/acme-api/internal/jobs/idempotencycleanup"
)

// defineIdempotencyCleanupJob declares the idempotency_cleanup job with its
// code defaults. Operators can override them in
// /ops/jobs/definitions/idempotency_cleanup.
func defineIdempotencyCleanupJob(defs *jobs.Definitions, deps jobDeps) {
	jobs.Define(defs, jobs.Definition[idempotencycleanup.Args]{
		Name:        idempotencycleanup.Name,
		Description: "Deletes idempotency keys and their stored responses once they are older than idempotency.retention.",
		Worker:      idempotencycleanup.NewWorker(deps.idempotencyCleanup, deps.logger),
		NewArgs:     func() idempotencycleanup.Args { return idempotencycleanup.Args{} },
		Enabled:     true,
		Schedule:    "@every 1h",
		Timeout:     5 * time.Minute,
		MaxAttempts: 3,
		Queue:       "default",
		Priority:    3,
	})
}
