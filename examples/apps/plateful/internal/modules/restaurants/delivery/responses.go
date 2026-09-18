package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// RestaurantResponse is a restaurant as the API returns it.
type RestaurantResponse struct {
	ID        string    `json:"id" example:"rst_mfrggzdfmztwq2lkmfrggzdfmy"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Cuisine   string    `json:"cuisine"`
	Status    string    `json:"status" enum:"onboarding,open,paused,suspended"`
	CreatedBy string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who created it"`
	Version   int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type restaurantOutput struct {
	Body RestaurantResponse
}

// restaurantIDInput is the path of the routes on one restaurant.
type restaurantIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"rst_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(restaurant domain.Restaurant) RestaurantResponse {
	return RestaurantResponse{
		ID:        restaurant.ID,
		Name:      restaurant.Name,
		Address:   restaurant.Address,
		Cuisine:   restaurant.Cuisine,
		Status:    string(restaurant.Status),
		CreatedBy: restaurant.CreatedBy,
		Version:   restaurant.Version,
		CreatedAt: restaurant.CreatedAt,
		UpdatedAt: restaurant.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the restaurant is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
