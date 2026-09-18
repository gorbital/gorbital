package delivery

import (
	"context"

	"example.com/plateful/internal/modules/couriers/domain"
	"example.com/plateful/internal/modules/couriers/usecase"
)

// docs:start register-courier-handler

type registerCourierInput struct {
	Body struct {
		DisplayName string `json:"display_name" minLength:"1" maxLength:"80" doc:"The name restaurants and diners see"`
		Vehicle     string `json:"vehicle" enum:"bicycle,scooter,car,on_foot"`
	}
}

// registerCourier creates the caller's own courier profile.
//
// The input has no owner in it, and that is the point: there is no {orgId}
// in the path and no user ID in the body, so there is nothing a request
// could say about whose profile this is. The use case takes the account out
// of the actor instead. A handler that accepted an ID here would be asking
// to be handed somebody else's.
func (h handlers) registerCourier(ctx context.Context, in *registerCourierInput) (*courierOutput, error) {
	c, err := h.svc.RegisterCourier(ctx, usecase.RegisterCourierInput{
		DisplayName: in.Body.DisplayName,
		Vehicle:     domain.Vehicle(in.Body.Vehicle),
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &courierOutput{Body: toResponse(c)}, nil
}

// docs:end register-courier-handler
