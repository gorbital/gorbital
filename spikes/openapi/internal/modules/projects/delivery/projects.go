// Package delivery is the HTTP adapter for projects. It is the only projects
// layer that imports Huma; handlers return domain errors unchanged and the
// application maps them to problem+json.
package delivery

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	projectdomain "gorbital.dev/spikes/openapi/internal/modules/projects/domain"
	projectusecase "gorbital.dev/spikes/openapi/internal/modules/projects/usecase"
)

// ProjectResponse is the public representation of a project.
type ProjectResponse struct {
	ID        string    `json:"id" doc:"Project ID" example:"prj_3f9a1c2b7d4e"`
	Name      string    `json:"name" doc:"Project name" example:"Website redesign"`
	Archived  bool      `json:"archived" doc:"Whether the project is archived"`
	CreatedAt time.Time `json:"created_at" doc:"Creation time (UTC)"`
}

// CreateProjectInput is the request for creating a project.
type CreateProjectInput struct {
	OrgID string `path:"orgId" doc:"Organisation ID" example:"org_acme"`
	Body  struct {
		Name string `json:"name" minLength:"1" maxLength:"100" doc:"Project name" example:"Website redesign"`
	}
}

// ProjectInput identifies one project.
type ProjectInput struct {
	OrgID string `path:"orgId" doc:"Organisation ID" example:"org_acme"`
	ID    string `path:"id" doc:"Project ID" example:"prj_3f9a1c2b7d4e"`
}

// ListProjectsInput identifies an organisation.
type ListProjectsInput struct {
	OrgID string `path:"orgId" doc:"Organisation ID" example:"org_acme"`
}

// ProjectOutput is a single-project response.
type ProjectOutput struct {
	Body ProjectResponse
}

// ProjectList is a list of projects.
type ProjectList struct {
	Items []ProjectResponse `json:"items"`
}

// ListProjectsOutput is the list response.
type ListProjectsOutput struct {
	Body ProjectList
}

type handler struct {
	svc *projectusecase.Service
}

// Register adds the projects operations to api. access returns the
// middleware enforcing a permission for the organisation in the path.
func Register(api huma.API, svc *projectusecase.Service, access func(permission string) huma.Middlewares) {
	h := &handler{svc: svc}
	security := []map[string][]string{{"bearer": {}}}
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound}

	huma.Register(api, huma.Operation{
		OperationID:   "create-project",
		Method:        http.MethodPost,
		Path:          "/v1/orgs/{orgId}/projects",
		Summary:       "Create a project",
		Tags:          []string{"Projects"},
		Security:      security,
		DefaultStatus: http.StatusCreated,
		Errors:        append(errs, http.StatusConflict, http.StatusUnprocessableEntity),
		Middlewares:   access("projects:create"),
	}, h.create)

	huma.Register(api, huma.Operation{
		OperationID: "list-projects",
		Method:      http.MethodGet,
		Path:        "/v1/orgs/{orgId}/projects",
		Summary:     "List projects",
		Tags:        []string{"Projects"},
		Security:    security,
		Errors:      errs,
		Middlewares: access("projects:read"),
	}, h.list)

	huma.Register(api, huma.Operation{
		OperationID: "get-project",
		Method:      http.MethodGet,
		Path:        "/v1/orgs/{orgId}/projects/{id}",
		Summary:     "Get a project",
		Tags:        []string{"Projects"},
		Security:    security,
		Errors:      errs,
		Middlewares: access("projects:read"),
	}, h.get)

	huma.Register(api, huma.Operation{
		OperationID: "archive-project",
		Method:      http.MethodPost,
		Path:        "/v1/orgs/{orgId}/projects/{id}/archive",
		Summary:     "Archive a project",
		Tags:        []string{"Projects"},
		Security:    security,
		Errors:      append(errs, http.StatusConflict),
		Middlewares: access("projects:update"),
	}, h.archive)
}

func (h *handler) create(ctx context.Context, in *CreateProjectInput) (*ProjectOutput, error) {
	p, err := h.svc.Create(ctx, in.OrgID, in.Body.Name)
	if err != nil {
		return nil, err
	}
	return &ProjectOutput{Body: toResponse(p)}, nil
}

func (h *handler) list(ctx context.Context, in *ListProjectsInput) (*ListProjectsOutput, error) {
	ps, err := h.svc.List(ctx, in.OrgID)
	if err != nil {
		return nil, err
	}
	out := &ListProjectsOutput{Body: ProjectList{Items: make([]ProjectResponse, 0, len(ps))}}
	for _, p := range ps {
		out.Body.Items = append(out.Body.Items, toResponse(p))
	}
	return out, nil
}

func (h *handler) get(ctx context.Context, in *ProjectInput) (*ProjectOutput, error) {
	p, err := h.svc.Get(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &ProjectOutput{Body: toResponse(p)}, nil
}

func (h *handler) archive(ctx context.Context, in *ProjectInput) (*ProjectOutput, error) {
	p, err := h.svc.Archive(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &ProjectOutput{Body: toResponse(p)}, nil
}

func toResponse(p projectdomain.Project) ProjectResponse {
	return ProjectResponse{ID: p.ID, Name: p.Name, Archived: p.Archived, CreatedAt: p.CreatedAt}
}
