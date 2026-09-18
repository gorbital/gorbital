package delivery

import "context"

type suspendRestaurantInput struct {
	ID   string `path:"id" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The restaurant's ID, which is its organisation's"`
	Body struct {
		Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the restaurant is suspended; the audit log keeps it"`
	}
}

func (h handlers) suspendRestaurant(ctx context.Context, in *suspendRestaurantInput) (*restaurantOutput, error) {
	r, err := h.svc.SuspendRestaurant(ctx, in.ID, in.Body.Reason)
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &restaurantOutput{Body: toResponse(r)}, nil
}

func (h handlers) liftSuspension(ctx context.Context, in *restaurantIDInput) (*restaurantOutput, error) {
	r, err := h.svc.LiftSuspension(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &restaurantOutput{Body: toResponse(r)}, nil
}
