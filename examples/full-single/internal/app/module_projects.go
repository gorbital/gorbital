package app

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/httpx"

	projectsmodule "example.com/acme-api/internal/modules/projects"
	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
	projectsusecase "example.com/acme-api/internal/modules/projects/usecase"
)

// projectsPermissions are the projects module's permissions.
// declarePermissions in permissions.go gives them to the user role, which
// every user holds.
var projectsPermissions = resourcePermissions{
	read: projectsusecase.PermRead, write: projectsusecase.PermWrite, name: "projects",
}

// registerProjects builds and wires the projects module. Error codes are
// public API: add new ones, never change existing ones.
func registerProjects(api huma.API, mapper *httpx.Mapper, svc services) error {
	err := mapper.Add(
		httpx.Mapping{Err: projectsdomain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		httpx.Mapping{Err: projectsdomain.ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Detail: "missing permission for this operation"},
		httpx.Mapping{Err: projectsdomain.ErrInvalidProject, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the project is not valid"},
		httpx.Mapping{Err: projectsdomain.ErrProjectNotFound, Status: http.StatusNotFound, Code: "project_not_found", Detail: "no project of yours has this ID"},
		httpx.Mapping{Err: projectsdomain.ErrProjectNameTaken, Status: http.StatusConflict, Code: "project_name_taken", Detail: "you already have a project with this name"},
		httpx.Mapping{Err: projectsdomain.ErrProjectVersionConflict, Status: http.StatusConflict, Code: "project_version_conflict", Detail: "the project changed since you read it; get it again and retry"},
	)
	if err != nil {
		return fmt.Errorf("projects module: %w", err)
	}
	// Without a database, when exporting the OpenAPI document, the operations
	// are registered without their dependencies.
	var m *projectsmodule.Module
	if svc.db != nil {
		m, err = projectsmodule.New(svc.db, projectsusecase.Config{Recorder: svc.recorder, Logger: svc.logger})
		if err != nil {
			return fmt.Errorf("projects module: %w", err)
		}
	}
	m.Register(api)
	return nil
}
