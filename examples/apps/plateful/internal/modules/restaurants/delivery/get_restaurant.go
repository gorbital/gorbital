package delivery

import "context"

type orgInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func (h handlers) getRestaurant(ctx context.Context, in *orgInput) (*restaurantOutput, error) {
	r, err := h.svc.GetRestaurant(ctx, in.OrgID)
	if err != nil {
		return nil, err
	}
	return &restaurantOutput{Body: toResponse(r)}, nil
}

type restaurantIDInput struct {
	ID string `path:"id" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The restaurant's ID, which is its organisation's"`
}

func (h handlers) viewRestaurant(ctx context.Context, in *restaurantIDInput) (*restaurantOutput, error) {
	r, err := h.svc.ViewRestaurant(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &restaurantOutput{Body: toResponse(r)}, nil
}
