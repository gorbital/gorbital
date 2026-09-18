package delivery

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

// docs:start save-restaurant-handler

type saveRestaurantInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Version         int64  `json:"version" minimum:"0" doc:"The version you read, or 0 to create the profile"`
		Name            string `json:"name" minLength:"1" maxLength:"100"`
		Address         string `json:"address" minLength:"1" maxLength:"200"`
		Cuisine         string `json:"cuisine,omitempty" maxLength:"60" example:"Neapolitan"`
		OpensMinute     int    `json:"opens_minute" minimum:"0" maximum:"1440" doc:"Minutes from midnight UTC"`
		ClosesMinute    int    `json:"closes_minute" minimum:"0" maximum:"1440" doc:"Minutes from midnight UTC"`
		DeliveryRadiusM int    `json:"delivery_radius_m" minimum:"1" doc:"How far you deliver, in metres"`
		CoverImageID    string `json:"cover_image_id,omitempty" maxLength:"64" doc:"An image of yours from POST /v1/orgs/{orgId}/images"`
		Status          string `json:"status,omitempty" enum:"onboarding,open,paused" default:"onboarding"`
	}
}

// saveRestaurant is the whole handler: read the request, call the use case,
// shape the answer. Every rule about what a restaurant may be, and who may
// set which status, is in the domain and the use case, where a job or a
// command could reach it too.
func (h handlers) saveRestaurant(ctx context.Context, in *saveRestaurantInput) (*restaurantOutput, error) {
	status := domain.Status(in.Body.Status)
	if status == "" {
		status = domain.StatusOnboarding
	}
	r, err := h.svc.SaveRestaurant(ctx, in.OrgID, usecase.SaveRestaurantInput{
		Version: in.Body.Version,
		Status:  status,
		Fields: domain.RestaurantFields{
			Name:            in.Body.Name,
			Address:         in.Body.Address,
			Cuisine:         in.Body.Cuisine,
			OpensMinute:     in.Body.OpensMinute,
			ClosesMinute:    in.Body.ClosesMinute,
			DeliveryRadiusM: in.Body.DeliveryRadiusM,
			CoverImageID:    in.Body.CoverImageID,
		},
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &restaurantOutput{Body: toResponse(r)}, nil
}

// docs:end save-restaurant-handler
