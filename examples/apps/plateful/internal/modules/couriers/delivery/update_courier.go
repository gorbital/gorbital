package delivery

import (
	"context"

	"example.com/plateful/internal/modules/couriers/domain"
	"example.com/plateful/internal/modules/couriers/usecase"
)

type updateCourierInput struct {
	Body struct {
		Version     int64   `json:"version" minimum:"1" doc:"The version you read. If the profile changed since, the update fails with courier_version_conflict."`
		DisplayName *string `json:"display_name,omitempty" minLength:"1" maxLength:"80"`
		Vehicle     *string `json:"vehicle,omitempty" enum:"bicycle,scooter,car,on_foot"`
		Available   *bool   `json:"available,omitempty" doc:"Whether you are on duty. You can't go off duty while you are carrying an order."`
	}
}

func (h handlers) updateCourier(ctx context.Context, in *updateCourierInput) (*courierOutput, error) {
	changes := domain.Changes{DisplayName: in.Body.DisplayName, Available: in.Body.Available}
	if in.Body.Vehicle != nil {
		value := domain.Vehicle(*in.Body.Vehicle)
		changes.Vehicle = &value
	}
	c, err := h.svc.UpdateCourier(ctx, usecase.UpdateCourierInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &courierOutput{Body: toResponse(c)}, nil
}
