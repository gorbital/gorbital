package app

import (
	authlib "gorbital.dev/modules/auth"

	authusecase "example.com/acme-api/internal/modules/auth/usecase"
	flagsusecase "example.com/acme-api/internal/modules/flags/usecase"
	opsdomain "example.com/acme-api/internal/modules/ops/domain"
)

// Platform roles (ADR-0038). Access is denied by default: a signed-in user
// holds only the permissions of their roles and of the user role
// (authusecase.RoleUser), which every user holds. Give someone a role with
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
	c.Permission(opsdomain.PermFlagsRead, "Read feature flags and their history")
	c.Permission(opsdomain.PermFlagsWrite, "Change and reset feature flags")
	c.Permission(opsdomain.PermJobsRead, "Read job definitions, runs and queues")
	c.Permission(opsdomain.PermJobsWrite, "Change job configuration; pause and resume queues")
	c.Permission(opsdomain.PermJobsRun, "Run, retry and cancel jobs")
	c.Permission(opsdomain.PermAuditRead, "Read the audit log")
	c.Permission(opsdomain.PermReleasesRead, "Read releases and the instances running them")
	c.Permission(opsdomain.PermMailRead, "See how the app sends email")
	c.Permission(opsdomain.PermMailTest, "Send a test email")
	c.Permission(opsdomain.PermMailWrite, "Remove addresses from the email suppression list")
	c.Permission(opsdomain.PermStorageRead, "Browse file storage")
	c.Permission(opsdomain.PermStorageWrite, "Upload, move and delete files and create signed URLs")
	c.Permission(opsdomain.PermAuthRead, "See which sign-in methods are configured")
	c.Permission(opsdomain.PermSystemRead, "See an instance's health checks, database pool, migrations and runtime")
	c.Permission(opsdomain.PermObservabilityRead, "See request rates, errors and latency across instances, and stream them")
	c.Permission(opsdomain.PermIncidentsRead, "Read incidents, their timelines and reports")
	c.Permission(opsdomain.PermIncidentsWrite, "Open, update and resolve incidents")
	c.Permission(authusecase.PermOpsAuthWrite, "Manage accounts: create, ban, delete, end sessions, remove passkeys and links, reset second factors, impersonate in development")
	c.Permission(authusecase.PermServiceAccountsRead, "See service accounts and their API keys")
	c.Permission(authusecase.PermServiceAccountsWrite, "Create, change and delete service accounts and their API keys")

	// Every user holds the user role without a grant (ADR-0058). It covers
	// what a signed-in user may do with their own data, so an API key's
	// scopes limit that too; sessions always have it.
	c.Permission(flagsusecase.PermFlagsRead, "Read the feature flags shown to clients")
	user := []string{flagsusecase.PermFlagsRead}
	for _, r := range userResourcePermissions() {
		c.Permission(r.read, "See your "+r.name)
		c.Permission(r.write, "Create, change and delete your "+r.name)
		user = append(user, r.read, r.write)
	}
	c.Role(authusecase.RoleUser, "Held by every signed-in user without a grant; by API keys only within their scopes", user...)

	c.Role(rolePlatformAdmin, "Operates the platform: every /ops permission",
		append(opsdomain.AllPermissions(), authusecase.PermOpsAuthWrite, authusecase.PermServiceAccountsRead, authusecase.PermServiceAccountsWrite)...)
	c.Role(roleOpsViewer, "Reads operational data without changing anything",
		opsdomain.PermSettingsRead, opsdomain.PermJobsRead, opsdomain.PermAuditRead, opsdomain.PermReleasesRead, opsdomain.PermMailRead, opsdomain.PermAuthRead, opsdomain.PermStorageRead,
		opsdomain.PermSystemRead, opsdomain.PermFlagsRead, opsdomain.PermObservabilityRead, opsdomain.PermIncidentsRead, authusecase.PermServiceAccountsRead)

	// Ops roles grant their permissions only to sessions signed in with a
	// second factor (ADR-0043), so never to API keys (ADR-0058).
	c.RequireMFA(rolePlatformAdmin, roleOpsViewer)
	return c
}

// resourcePermissions are a resource's permissions, declared in its
// module_<name>.go file.
type resourcePermissions struct {
	read, write string
	name        string // in words, such as "projects"
}

// userResourcePermissions lists the user-scoped resources' permissions,
// which the user role grants. orb gen resource --scope user adds a line at
// the anchor.
func userResourcePermissions() []resourcePermissions {
	return []resourcePermissions{
		//orb:anchor user-permissions
		projectsPermissions,
	}
}

// permissionCatalogs returns every permission catalog by name, for the
// public-surface inventory in api/surface.json (ADR-0054).
func permissionCatalogs() map[string]*authlib.Catalog {
	return map[string]*authlib.Catalog{"platform": declarePermissions()}
}
