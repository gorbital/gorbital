// Package domain holds the operations module's permissions and errors. It
// imports only the standard library.
package domain

import "errors"

// Permissions for operations APIs. Permission names are public API.
const (
	PermSettingsRead  = "ops.settings.read"
	PermSettingsWrite = "ops.settings.write"
	PermJobsRead      = "ops.jobs.read"
	PermJobsWrite     = "ops.jobs.write"
	PermJobsRun       = "ops.jobs.run"
)

// AllPermissions returns every operations permission.
func AllPermissions() []string {
	return []string{PermSettingsRead, PermSettingsWrite, PermJobsRead, PermJobsWrite, PermJobsRun}
}

// Errors returned by operations use cases.
var (
	ErrUnauthenticated = errors.New("authentication is required")
	ErrForbidden       = errors.New("missing permission")
)
