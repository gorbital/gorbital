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
	ID              string `json:"id" example:"rst_mfrggzdfmztwq2lkmfrggzdfmy"`
	Name            string `json:"name"`
	Address         string `json:"address"`
	Cuisine         string `json:"cuisine"`
	OpensMinute     int    `json:"opens_minute" doc:"Minutes from midnight UTC, 0 to 1440"`
	ClosesMinute    int    `json:"closes_minute" doc:"Minutes from midnight UTC, 0 to 1440"`
	DeliveryRadiusM int    `json:"delivery_radius_m" doc:"How far it delivers, in metres"`
	Status          string `json:"status" enum:"onboarding,open,paused,suspended"`
	CoverImageID    string `json:"cover_image_id,omitempty" doc:"The images module's image shown as the cover"`
	// SuspendedReason is empty for customers: only the restaurant's own
	// staff and the platform's see why it was suspended.
	SuspendedReason string    `json:"suspended_reason,omitempty"`
	CreatedBy       string    `json:"created_by" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The member who created the profile"`
	Version         int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type restaurantOutput struct {
	Body RestaurantResponse
}

func toResponse(r domain.Restaurant) RestaurantResponse {
	return RestaurantResponse{
		ID:              r.ID,
		Name:            r.Name,
		Address:         r.Address,
		Cuisine:         r.Cuisine,
		OpensMinute:     r.OpensMinute,
		ClosesMinute:    r.ClosesMinute,
		DeliveryRadiusM: r.DeliveryRadiusM,
		Status:          string(r.Status),
		CoverImageID:    r.CoverImageID,
		SuspendedReason: r.SuspendedReason,
		CreatedBy:       r.CreatedBy,
		Version:         r.Version,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
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
