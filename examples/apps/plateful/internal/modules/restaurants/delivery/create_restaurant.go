package delivery

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
)

type createRestaurantInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Name    string `json:"name" minLength:"1" maxLength:"100" doc:"Unique in the organisation, ignoring case"`
		Address string `json:"address" minLength:"1" maxLength:"100"`
		Cuisine string `json:"cuisine" minLength:"1" maxLength:"100"`
		Status  string `json:"status,omitempty" enum:"onboarding,open,paused,suspended" default:"onboarding"`
	}
}

func (h handlers) createRestaurant(ctx context.Context, in *createRestaurantInput) (*restaurantOutput, error) {
	restaurant, err := h.svc.CreateRestaurant(ctx, in.OrgID, domain.RestaurantFields{
		Name:    in.Body.Name,
		Address: in.Body.Address,
		Cuisine: in.Body.Cuisine,
		Status:  domain.Status(in.Body.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &restaurantOutput{Body: toResponse(restaurant)}, nil
}
