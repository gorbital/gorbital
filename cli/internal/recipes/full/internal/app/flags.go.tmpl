package app

import (
	"gorbital.dev/modules/flags"
)

// appFlags are the feature flags: features operators turn on, target at
// organisations and users, and roll out to a percentage of them through
// /ops/flags without a redeploy (ADR-0057). Pass a flag to a module as a
// config.Value[bool], or call Enabled(ctx) where the feature branches.
type appFlags struct {
	pingTime *flags.Flag
}

// declareFlags declares every feature flag. Flag keys are public API: add
// new ones, and remove a flag's code before its key.
func declareFlags(reg *flags.Registry) appFlags {
	return appFlags{
		pingTime: flags.Bool(reg, "example.ping_time",
			flags.Describe("Adds the server's time to GET /v1/ping replies. An example feature flag: turn it on with PUT /ops/flags/example.ping_time."),
			flags.Client(), // listed by GET /v1/flags
		),
	}
}
