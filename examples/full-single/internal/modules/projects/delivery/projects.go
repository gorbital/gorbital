// Package delivery is the projects module's HTTP adapter: Huma
// operations and request and response types for /v1/projects.
package delivery

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/page"

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
	projectsusecase "example.com/acme-api/internal/modules/projects/usecase"
)

// ProjectResponse is a project.
type ProjectResponse struct {
	ID          string    `json:"id" example:"prj_mfrggzdfmztwq2lkmfrggzdfmy"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status" enum:"active,archived"`
	Version     int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectPage is a page of projects.
type ProjectPage struct {
	Items      []ProjectResponse `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type projectOutput struct{ Body ProjectResponse }

type projectPageOutput struct{ Body ProjectPage }

type createInput struct {
	Body struct {
		_           struct{} `json:"-" additionalProperties:"true"`
		Name        string   `json:"name" maxLength:"100" doc:"Unique among your projects, ignoring case"`
		Description string   `json:"description,omitempty" maxLength:"2000"`
		Status      string   `json:"status,omitempty" enum:"active,archived" default:"active"`
	}
}

type listInput struct {
	page.Params
	Status string `query:"status" enum:"active,archived" doc:"Only projects with this status"`
}

type idInput struct {
	ID string `path:"id" maxLength:"64" example:"prj_mfrggzdfmztwq2lkmfrggzdfmy"`
}

type updateInput struct {
	ID   string `path:"id" maxLength:"64" example:"prj_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body struct {
		_           struct{} `json:"-" additionalProperties:"true"`
		Version     int64    `json:"version" minimum:"1" doc:"The version you read. If the project changed since, the update fails with project_version_conflict."`
		Name        *string  `json:"name,omitempty" maxLength:"100"`
		Description *string  `json:"description,omitempty" maxLength:"2000"`
		Status      *string  `json:"status,omitempty" enum:"active,archived"`
	}
}

type handler struct {
	svc *projectsusecase.Service
}

// Register adds the projects operations to api. Every operation needs a
// signed-in user and reaches only that user's projects.
func Register(api huma.API, svc *projectsusecase.Service) {
	h := &handler{svc: svc}
	signedIn := func(op huma.Operation) huma.Operation {
		op.Tags, op.Security = []string{"Projects"}, openapi.Bearer
		op.Errors = append([]int{http.StatusUnauthorized}, op.Errors...)
		return op
	}

	huma.Register(api, signedIn(huma.Operation{
		OperationID: "projects-create", Method: http.MethodPost, Path: "/v1/projects",
		Summary: "Create a project", DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.create)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "projects-list", Method: http.MethodGet, Path: "/v1/projects",
		Summary:     "List your projects",
		Description: "Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at` or `name`. Paginate with `cursor`.",
		Errors:      []int{http.StatusBadRequest, http.StatusUnprocessableEntity},
	}), h.list)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "projects-get", Method: http.MethodGet, Path: "/v1/projects/{id}",
		Summary: "Get a project", Errors: []int{http.StatusNotFound},
	}), h.get)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "projects-update", Method: http.MethodPatch, Path: "/v1/projects/{id}",
		Summary:     "Update a project",
		Description: "Send the fields to change and the `version` you read.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}), h.update)
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "projects-delete", Method: http.MethodDelete, Path: "/v1/projects/{id}",
		Summary: "Delete a project", DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusNotFound},
	}), h.delete)
}

func (h *handler) create(ctx context.Context, in *createInput) (*projectOutput, error) {
	p, err := h.svc.Create(ctx, projectsdomain.ProjectFields{
		Name:        in.Body.Name,
		Description: in.Body.Description,
		Status:      projectsdomain.Status(in.Body.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &projectOutput{Body: projectResponse(p)}, nil
}

func (h *handler) list(ctx context.Context, in *listInput) (*projectPageOutput, error) {
	res, err := h.svc.List(ctx, projectsusecase.ListInput{
		Page:   in.Params,
		Status: projectsdomain.Status(in.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &projectPageOutput{Body: ProjectPage{Items: make([]ProjectResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, p := range res.Items {
		out.Body.Items[i] = projectResponse(p)
	}
	return out, nil
}

func (h *handler) get(ctx context.Context, in *idInput) (*projectOutput, error) {
	p, err := h.svc.Get(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &projectOutput{Body: projectResponse(p)}, nil
}

func (h *handler) update(ctx context.Context, in *updateInput) (*projectOutput, error) {
	changes := projectsdomain.Changes{
		Name:        in.Body.Name,
		Description: in.Body.Description,
	}
	if in.Body.Status != nil {
		value := projectsdomain.Status(*in.Body.Status)
		changes.Status = &value
	}
	p, err := h.svc.Update(ctx, in.ID, projectsusecase.UpdateInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &projectOutput{Body: projectResponse(p)}, nil
}

func (h *handler) delete(ctx context.Context, in *idInput) (*struct{}, error) {
	return nil, h.svc.Delete(ctx, in.ID)
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// other errors are mapped in internal/app/module_projects.go.
func fieldErrors(err error, location string) error {
	var invalid *projectsdomain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the project is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}

func projectResponse(p projectsdomain.Project) ProjectResponse {
	return ProjectResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Status:      string(p.Status),
		Version:     p.Version,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}
