package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/projects/domain"
)

// ProjectResponse is a project as the API returns it.
type ProjectResponse struct {
	ID          string    `json:"id" example:"prj_mfrggzdfmztwq2lkmfrggzdfmy"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status" enum:"active,archived"`
	CreatedBy   string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who created it"`
	Version     int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type projectOutput struct {
	Body ProjectResponse
}

// projectIDInput is the path of the routes on one project.
type projectIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"prj_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(project domain.Project) ProjectResponse {
	return ProjectResponse{
		ID:          project.ID,
		Name:        project.Name,
		Description: project.Description,
		Status:      string(project.Status),
		CreatedBy:   project.CreatedBy,
		Version:     project.Version,
		CreatedAt:   project.CreatedAt,
		UpdatedAt:   project.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the project is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
