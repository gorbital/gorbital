package app

import (
	authlib "gorbital.dev/modules/auth"

	authusecase "example.com/acme-api/internal/modules/auth/usecase"
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
	c.Permission(opsdomain.PermReleasesRead, "Read releases and the instances running them")
	c.Permission(opsdomain.PermMailRead, "See how the app sends email")
	c.Permission(opsdomain.PermMailTest, "Send a test email")
	c.Permission(opsdomain.PermMailWrite, "Remove addresses from the email suppression list")
	c.Permission(opsdomain.PermAuthRead, "See which sign-in methods are configured")
	c.Permission(opsdomain.PermSystemRead, "See an instance's health checks, database pool, migrations and runtime")
	c.Permission(authusecase.PermServiceAccountsRead, "See service accounts and their API keys")
	c.Permission(authusecase.PermServiceAccountsWrite, "Create, change and delete service accounts and their API keys")

	c.Role(rolePlatformAdmin, "Operates the platform: every /ops permission",
		append(opsdomain.AllPermissions(), authusecase.PermServiceAccountsRead, authusecase.PermServiceAccountsWrite)...)
	c.Role(roleOpsViewer, "Reads operational data without changing anything",
		opsdomain.PermSettingsRead, opsdomain.PermJobsRead, opsdomain.PermAuditRead, opsdomain.PermReleasesRead, opsdomain.PermMailRead, opsdomain.PermAuthRead,
		opsdomain.PermSystemRead, authusecase.PermServiceAccountsRead)

	// Ops roles grant their permissions only to sessions signed in with a
	// second factor (ADR-0043), so never to API keys (ADR-0058).
	c.RequireMFA(rolePlatformAdmin, roleOpsViewer)
	return c
}

// permissionCatalogs returns every permission catalog by name, for the
// public-surface inventory in api/surface.json (ADR-0054).
func permissionCatalogs() map[string]*authlib.Catalog {
	return map[string]*authlib.Catalog{"platform": declarePermissions()}
}
