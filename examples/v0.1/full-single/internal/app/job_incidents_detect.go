package app

import (
	"time"

	"gorbital.dev/modules/jobs"

	"example.com/acme-api/internal/jobs/incidentsdetect"
)

// defineIncidentsDetectJob declares the incidents_detect job with its code
// defaults. Operators can override them in
// /ops/jobs/definitions/incidents_detect; disabling it stops automatic
// incidents.
func defineIncidentsDetectJob(defs *jobs.Definitions, deps jobDeps) {
	jobs.Define(defs, jobs.Definition[incidentsdetect.Args]{
		Name:        incidentsdetect.Name,
		Description: "Opens an automatic incident when the server error rate over incidents.detection_window is above incidents.error_rate_threshold, and notes when it recovers.",
		Worker:      incidentsdetect.NewWorker(deps.detectIncidents, deps.recorder, deps.logger),
		NewArgs:     func() incidentsdetect.Args { return incidentsdetect.Args{} },
		Enabled:     true,
		Schedule:    "@every 1m",
		Timeout:     30 * time.Second,
		MaxAttempts: 1, // the next minute's run detects again
		Queue:       "default",
		Priority:    2,
	})
}
