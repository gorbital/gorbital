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
	PermAuditRead     = "ops.audit.read"
	PermReleasesRead  = "ops.releases.read"
	PermMailRead      = "ops.mail.read"
	PermMailTest      = "ops.mail.test"
	PermMailWrite     = "ops.mail.write"
	PermAuthRead      = "ops.auth.read"
	PermSystemRead    = "ops.system.read"

	PermObservabilityRead = "ops.observability.read"
	PermIncidentsRead     = "ops.incidents.read"
	PermIncidentsWrite    = "ops.incidents.write"
)

// AllPermissions returns every operations permission.
func AllPermissions() []string {
	return []string{PermSettingsRead, PermSettingsWrite, PermJobsRead, PermJobsWrite, PermJobsRun, PermAuditRead, PermReleasesRead, PermMailRead, PermMailTest, PermMailWrite, PermAuthRead, PermSystemRead,
		PermObservabilityRead, PermIncidentsRead, PermIncidentsWrite}
}

// Errors returned by operations use cases.
var (
	ErrUnauthenticated   = errors.New("authentication is required")
	ErrForbidden         = errors.New("missing permission")
	ErrMFARequired       = errors.New("the permission needs a session signed in with two-factor authentication")
	ErrInvalidRecipient  = errors.New("recipient is not an email address")
	ErrTooManyTestEmails = errors.New("too many test emails")
	// ErrSuppressionReasonRequired reports removing an address from the
	// suppression list without saying why.
	ErrSuppressionReasonRequired = errors.New("a reason is required to remove a suppression")
	// ErrInvalidWindow reports an observability window that isn't a whole
	// number of minutes from 1 minute to 24 hours.
	ErrInvalidWindow = errors.New("the window must be whole minutes from 1 minute to 24 hours")
)
