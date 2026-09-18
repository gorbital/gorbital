// Package projects is the projects module: projects that belong to an
// organisation, whose members reach them through their role, in four layers
// (domain, usecase, repository, delivery) with one file per operation in
// each. orb gen module wrote it; the code is yours to change.
package projects

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/projects/delivery"
	"example.com/plateful/internal/modules/projects/domain"
	"example.com/plateful/internal/modules/projects/repository"
	"example.com/plateful/internal/modules/projects/usecase"
)

// Module returns the projects module. main.go adds it with every other
// module through modules.All; its routes need the organisations module
// (orgshttp.Module), which answers guard.OrgMember. Error codes and
// permission names are public API: add new ones, never change existing
// ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "projects",
		// guard.OrgMember answers org_not_found for an organisation the caller
		// isn't a member of, and forbidden for a role without the permission.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidProject, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the project is not valid"},
			{Err: domain.ErrProjectNotFound, Status: http.StatusNotFound, Code: "project_not_found", Detail: "the organisation has no project with this ID"},
			{Err: domain.ErrProjectNameTaken, Status: http.StatusConflict, Code: "project_name_taken", Detail: "the organisation already has a project with this name"},
			{Err: domain.ErrProjectVersionConflict, Status: http.StatusConflict, Code: "project_version_conflict", Detail: "the project changed since you read it; get it again and retry"},
		},
		// Organisation permissions: every member holds them through their
		// role in the organisation, an API key only when its scopes include
		// them. Platform roles grant nothing in an organisation.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the organisation's projects", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermWrite, Description: "Create, change and delete the organisation's projects", OrgRoles: []string{"owner", "admin", "member"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
