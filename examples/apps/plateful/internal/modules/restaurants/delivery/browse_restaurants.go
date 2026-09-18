package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/restaurants/usecase"
)

type browseRestaurantsInput struct {
	page.Params
	Cuisine string `query:"cuisine" maxLength:"60" doc:"Only restaurants of this cuisine"`
}

// RestaurantPage is a page of restaurants.
type RestaurantPage struct {
	Items      []RestaurantResponse `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type restaurantPageOutput struct {
	Body RestaurantPage
}

func (h handlers) browseRestaurants(ctx context.Context, in *browseRestaurantsInput) (*restaurantPageOutput, error) {
	return h.browse(ctx, usecase.BrowseInput{Page: in.Params, Cuisine: in.Cuisine})
}

func (h handlers) overseeRestaurants(ctx context.Context, in *browseRestaurantsInput) (*restaurantPageOutput, error) {
	return h.browse(ctx, usecase.BrowseInput{Page: in.Params, Cuisine: in.Cuisine, All: true})
}

func (h handlers) browse(ctx context.Context, in usecase.BrowseInput) (*restaurantPageOutput, error) {
	res, err := h.svc.BrowseRestaurants(ctx, in)
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &restaurantPageOutput{Body: RestaurantPage{Items: make([]RestaurantResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, r := range res.Items {
		out.Body.Items[i] = toResponse(r)
	}
	return out, nil
}
