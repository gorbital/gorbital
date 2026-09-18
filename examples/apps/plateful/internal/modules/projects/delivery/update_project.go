package delivery

import (
	"context"

	"example.com/plateful/internal/modules/projects/domain"
	"example.com/plateful/internal/modules/projects/usecase"
)

type updateProjectInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"prj_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Version     int64   `json:"version" minimum:"1" doc:"The version you read. If the project changed since, the update fails with project_version_conflict."`
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100"`
		Description *string `json:"description,omitempty" maxLength:"2000"`
		Status      *string `json:"status,omitempty" enum:"active,archived"`
	}
}

func (h handlers) updateProject(ctx context.Context, in *updateProjectInput) (*projectOutput, error) {
	changes := domain.Changes{
		Name:        in.Body.Name,
		Description: in.Body.Description,
	}
	if in.Body.Status != nil {
		value := domain.Status(*in.Body.Status)
		changes.Status = &value
	}
	project, err := h.svc.UpdateProject(ctx, in.OrgID, in.ID, usecase.UpdateProjectInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &projectOutput{Body: toResponse(project)}, nil
}
