package app

import (
	authlib "apistock.dev/modules/auth"
	orgslib "apistock.dev/modules/orgs"

	opsdomain "example.com/acme-api/internal/modules/ops/domain"
	orgsusecase "example.com/acme-api/internal/modules/orgs/usecase"
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
	c.Permission(opsdomain.PermAuthRead, "See which sign-in methods are configured")
	c.Permission(opsdomain.PermSystemRead, "See an instance's health checks, database pool, migrations and runtime")

	c.Role(rolePlatformAdmin, "Operates the platform: every /ops permission", opsdomain.AllPermissions()...)
	c.Role(roleOpsViewer, "Reads operational data without changing anything",
		opsdomain.PermSettingsRead, opsdomain.PermJobsRead, opsdomain.PermAuditRead, opsdomain.PermReleasesRead, opsdomain.PermMailRead, opsdomain.PermAuthRead,
		opsdomain.PermSystemRead)

	// Ops roles grant their permissions only to sessions signed in with a
	// second factor (ADR-0043).
	c.RequireMFA(rolePlatformAdmin, roleOpsViewer)
	return c
}

// resourcePermissions are an org-scoped resource's permissions, declared in
// its module_<name>.go file.
type resourcePermissions struct {
	read, write string
	name        string // in words, such as "projects"
}

// orgResourcePermissions lists the org-scoped resources' permissions.
// aps gen resource --scope org adds a line at the anchor.
func orgResourcePermissions() []resourcePermissions {
	return []resourcePermissions{
		//aps:anchor org-permissions
		projectsPermissions,
	}
}

// declareOrgPermissions declares the permissions organisation members hold
// through their role in each organisation (ADR-0048). They never grant
// /ops access, and platform roles never grant them. Every member has exactly
// one role, and every role works with the org-scoped resources; add roles
// here for your product, such as a read-only "viewer".
func declareOrgPermissions() *authlib.Catalog {
	c := authlib.NewCatalog()
	c.Permission(orgsusecase.PermOrgRead, "See the organisation")
	c.Permission(orgsusecase.PermOrgUpdate, "Rename the organisation")
	c.Permission(orgsusecase.PermOrgDelete, "Delete and restore the organisation")
	c.Permission(orgsusecase.PermMembersRead, "See the members")
	c.Permission(orgsusecase.PermMembersManage, "Invite people, change roles and remove members, up to your own role")

	member := []string{orgsusecase.PermOrgRead, orgsusecase.PermMembersRead}
	for _, r := range orgResourcePermissions() {
		c.Permission(r.read, "See "+r.name)
		c.Permission(r.write, "Create, change and delete "+r.name)
		member = append(member, r.read, r.write)
	}
	admin := append([]string{orgsusecase.PermOrgUpdate, orgsusecase.PermMembersManage}, member...)
	c.Role(orgslib.RoleOwner, "Everything, including deleting the organisation and managing owners", append([]string{orgsusecase.PermOrgDelete}, admin...)...)
	c.Role(orgslib.RoleAdmin, "Manages the organisation and its members, except owners", admin...)
	c.Role(orgslib.RoleMember, "Works in the organisation", member...)
	return c
}
