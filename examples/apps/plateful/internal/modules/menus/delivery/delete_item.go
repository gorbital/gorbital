package delivery

import "context"

func (h handlers) deleteItem(ctx context.Context, in *itemIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteItem(ctx, in.OrgID, in.ID)
}
