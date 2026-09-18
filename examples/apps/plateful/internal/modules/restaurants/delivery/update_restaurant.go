package delivery

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

type updateRestaurantInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"rst_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Version int64   `json:"version" minimum:"1" doc:"The version you read. If the restaurant changed since, the update fails with restaurant_version_conflict."`
		Name    *string `json:"name,omitempty" minLength:"1" maxLength:"100"`
		Address *string `json:"address,omitempty" minLength:"1" maxLength:"100"`
		Cuisine *string `json:"cuisine,omitempty" minLength:"1" maxLength:"100"`
		Status  *string `json:"status,omitempty" enum:"onboarding,open,paused,suspended"`
	}
}

func (h handlers) updateRestaurant(ctx context.Context, in *updateRestaurantInput) (*restaurantOutput, error) {
	changes := domain.Changes{
		Name:    in.Body.Name,
		Address: in.Body.Address,
		Cuisine: in.Body.Cuisine,
	}
	if in.Body.Status != nil {
		value := domain.Status(*in.Body.Status)
		changes.Status = &value
	}
	restaurant, err := h.svc.UpdateRestaurant(ctx, in.OrgID, in.ID, usecase.UpdateRestaurantInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &restaurantOutput{Body: toResponse(restaurant)}, nil
}
