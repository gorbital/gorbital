package delivery

import "context"

func (h handlers) deleteProject(ctx context.Context, in *projectIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteProject(ctx, in.OrgID, in.ID)
}
