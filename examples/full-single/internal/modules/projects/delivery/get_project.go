package delivery

import "context"

func (h handlers) getProject(ctx context.Context, in *projectIDInput) (*projectOutput, error) {
	project, err := h.svc.GetProject(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &projectOutput{Body: toResponse(project)}, nil
}
