package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/projects/domain"
	"example.com/plateful/internal/modules/projects/usecase"
)

type listProjectsInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	page.Params
	Status string `query:"status" enum:"active,archived" doc:"Only projects with this status"`
}

// ProjectPage is a page of projects.
type ProjectPage struct {
	Items      []ProjectResponse `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type projectPageOutput struct {
	Body ProjectPage
}

func (h handlers) listProjects(ctx context.Context, in *listProjectsInput) (*projectPageOutput, error) {
	res, err := h.svc.ListProjects(ctx, in.OrgID, usecase.ListProjectsInput{
		Page:   in.Params,
		Status: domain.Status(in.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &projectPageOutput{Body: ProjectPage{Items: make([]ProjectResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, project := range res.Items {
		out.Body.Items[i] = toResponse(project)
	}
	return out, nil
}
