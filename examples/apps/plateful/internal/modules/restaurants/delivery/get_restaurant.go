package delivery

import "context"

func (h handlers) getRestaurant(ctx context.Context, in *restaurantIDInput) (*restaurantOutput, error) {
	restaurant, err := h.svc.GetRestaurant(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &restaurantOutput{Body: toResponse(restaurant)}, nil
}
