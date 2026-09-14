package app

import (
	authlib "apistock.dev/modules/auth"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// Platform roles (ADR-0038). Access is denied by default: a signed-in user
// holds only the permissions of their roles. Give someone a role with
//
//	go run ./cmd/api grant-role <email> <role>
//
// Role and permission names are public API.
const (
	rolePlatformAdmin = "platform_admin"
	roleOpsViewer     = "ops_viewer"
)

// declarePermissions declares every permission the modules check and the
// roles that grant them.
func declarePermissions() *authlib.Catalog {
	c := authlib.NewCatalog()
	c.Permission(opsdomain.PermSettingsRead, "Read runtime settings and their history")
	c.Permission(opsdomain.PermSettingsWrite, "Change and reset runtime settings")
	c.Permission(opsdomain.PermJobsRead, "Read job definitions, runs and queues")
	c.Permission(opsdomain.PermJobsWrite, "Change job configuration; pause and resume queues")
	c.Permission(opsdomain.PermJobsRun, "Run, retry and cancel jobs")
	c.Permission(opsdomain.PermAuditRead, "Read the audit log")
	c.Permission(opsdomain.PermMailRead, "See how the app sends email")
	c.Permission(opsdomain.PermMailTest, "Send a test email")

	c.Role(rolePlatformAdmin, "Operates the platform: every /ops permission", opsdomain.AllPermissions()...)
	c.Role(roleOpsViewer, "Reads operational data without changing anything",
		opsdomain.PermSettingsRead, opsdomain.PermJobsRead, opsdomain.PermAuditRead, opsdomain.PermMailRead)
	return c
}
