package delivery

import "context"

func (h handlers) deleteRestaurant(ctx context.Context, in *restaurantIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteRestaurant(ctx, in.OrgID, in.ID)
}
