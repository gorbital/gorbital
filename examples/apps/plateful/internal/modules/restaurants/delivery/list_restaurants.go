package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/restaurants/domain"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

type listRestaurantsInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	page.Params
	Status string `query:"status" enum:"onboarding,open,paused,suspended" doc:"Only restaurants with this status"`
}

// RestaurantPage is a page of restaurants.
type RestaurantPage struct {
	Items      []RestaurantResponse `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type restaurantPageOutput struct {
	Body RestaurantPage
}

func (h handlers) listRestaurants(ctx context.Context, in *listRestaurantsInput) (*restaurantPageOutput, error) {
	res, err := h.svc.ListRestaurants(ctx, in.OrgID, usecase.ListRestaurantsInput{
		Page:   in.Params,
		Status: domain.Status(in.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &restaurantPageOutput{Body: RestaurantPage{Items: make([]RestaurantResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, restaurant := range res.Items {
		out.Body.Items[i] = toResponse(restaurant)
	}
	return out, nil
}
