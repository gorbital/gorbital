package delivery

import "context"

func (h handlers) deleteOrder(ctx context.Context, in *orderIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteOrder(ctx, in.OrgID, in.ID)
}
