package app

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/httpx"
	"apistock.dev/page"

	projectsmodule "example.com/acme-api/internal/modules/projects"
	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
)

// registerProjects wires the projects module. Error codes are public API:
// add new ones, never change existing ones.
func registerProjects(api huma.API, mapper *httpx.Mapper, m *projectsmodule.Module) error {
	err := mapper.Add(
		httpx.Mapping{Err: projectsdomain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		httpx.Mapping{Err: projectsdomain.ErrInvalidProject, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the project is not valid"},
		httpx.Mapping{Err: projectsdomain.ErrProjectNotFound, Status: http.StatusNotFound, Code: "project_not_found", Detail: "no project of yours has this ID"},
		httpx.Mapping{Err: projectsdomain.ErrProjectNameTaken, Status: http.StatusConflict, Code: "project_name_taken", Detail: "you already have a project with this name"},
		httpx.Mapping{Err: projectsdomain.ErrProjectVersionConflict, Status: http.StatusConflict, Code: "project_version_conflict", Detail: "the project changed since you read it; get it again and retry"},
		httpx.Mapping{Err: page.ErrInvalidCursor, Status: http.StatusBadRequest, Code: "invalid_cursor", Detail: "the cursor is not valid"},
		httpx.Mapping{Err: page.ErrInvalidSort, Status: http.StatusBadRequest, Code: "invalid_sort", Detail: "sort by one of created_at, updated_at or name, with - for descending order"},
		httpx.Mapping{Err: page.ErrInvalidLimit, Status: http.StatusBadRequest, Code: "invalid_limit", Detail: "limit must be between 1 and 100"},
	)
	if err != nil {
		return fmt.Errorf("projects module: %w", err)
	}
	m.Register(api)
	return nil
}
