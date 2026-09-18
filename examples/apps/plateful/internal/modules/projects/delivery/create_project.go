package delivery

import (
	"context"

	"example.com/plateful/internal/modules/projects/domain"
)

type createProjectInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Name        string `json:"name" minLength:"1" maxLength:"100" doc:"Unique in the organisation, ignoring case"`
		Description string `json:"description,omitempty" maxLength:"2000"`
		Status      string `json:"status,omitempty" enum:"active,archived" default:"active"`
	}
}

func (h handlers) createProject(ctx context.Context, in *createProjectInput) (*projectOutput, error) {
	project, err := h.svc.CreateProject(ctx, in.OrgID, domain.ProjectFields{
		Name:        in.Body.Name,
		Description: in.Body.Description,
		Status:      domain.Status(in.Body.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &projectOutput{Body: toResponse(project)}, nil
}
