// Package projects is the projects module: projects that belong to the
// signed-in user, in four layers (domain, usecase, repository, delivery)
// with one file per operation in each. orb gen module wrote it; the code is
// yours to change.
package projects

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/acme-api/internal/modules/projects/delivery"
	"example.com/acme-api/internal/modules/projects/domain"
	"example.com/acme-api/internal/modules/projects/repository"
	"example.com/acme-api/internal/modules/projects/usecase"
)

// Module returns the projects module. main.go adds it with every other
// module through modules.All. Error codes and permission names are public
// API: add new ones, never change existing ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "projects",
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidProject, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the project is not valid"},
			{Err: domain.ErrProjectNotFound, Status: http.StatusNotFound, Code: "project_not_found", Detail: "no project of yours has this ID"},
			{Err: domain.ErrProjectNameTaken, Status: http.StatusConflict, Code: "project_name_taken", Detail: "you already have a project with this name"},
			{Err: domain.ErrProjectVersionConflict, Status: http.StatusConflict, Code: "project_version_conflict", Detail: "the project changed since you read it; get it again and retry"},
		},
		// Every user holds these through the user role; an API key only when
		// its scopes include them.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See your projects", Roles: []string{"user"}},
			{Name: usecase.PermWrite, Description: "Create, change and delete your projects", Roles: []string{"user"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
