package delivery

import "context"

func (h handlers) deleteTrip(ctx context.Context, in *tripIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteTrip(ctx, in.ID)
}
