package app

import (
	"time"

	"gorbital.dev/modules/jobs"

	"example.com/acme-api/internal/jobs/heartbeat"
)

// defineHeartbeatJob declares the heartbeat job with its code defaults.
// Operators can override them in /ops/jobs/definitions/heartbeat.
//
// The orb:job line records how the job was generated, so the Dev Portal
// can show it as a form until the worker is edited by hand (ADR-0071).
//
//orb:job {"kind":"custom","worker":"sha256:a6af0ce63cb3e491834f44231d90c59ba26b8b40cadb92d6c691a71355cd7199"}
func defineHeartbeatJob(defs *jobs.Definitions, deps jobDeps) {
	jobs.Define(defs, jobs.Definition[heartbeat.Args]{
		Name:        heartbeat.Name,
		Description: "Logs a heartbeat. An example job: change its schedule in /ops/jobs.",
		Worker:      heartbeat.NewWorker(deps.logger),
		NewArgs:     func() heartbeat.Args { return heartbeat.Args{} },
		Enabled:     true,
		Schedule:    "@every 1h",
		Timeout:     time.Minute,
		MaxAttempts: 3,
		Queue:       "default",
		Priority:    1,
	})
}
