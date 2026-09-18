package delivery

import "context"

type assignCourierInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		CourierID string `json:"courier_id" minLength:"1" maxLength:"64" example:"cur_mfrggzdfmztwq2lkmfrggzdfmy"`
	}
}

func (h handlers) assignCourier(ctx context.Context, in *assignCourierInput) (*orderOutput, error) {
	order, err := h.svc.AssignCourier(ctx, in.OrgID, in.ID, in.Body.CourierID)
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &orderOutput{Body: toResponse(order)}, nil
}

// FreeCourierResponse is a courier as a restaurant sees one while choosing
// who to give an order to. It carries nothing else: a courier is not the
// restaurant's, and their account, their availability and what else they are
// carrying are none of its business.
//
// The name is not CourierResponse because the couriers module's own delivery
// package already has one, and Huma registers a schema under its type's bare
// name: two types called CourierResponse in one app collide when the routes
// are mounted, whatever packages they are in.
type FreeCourierResponse struct {
	ID          string `json:"id" example:"cur_mfrggzdfmztwq2lkmfrggzdfmy"`
	DisplayName string `json:"display_name"`
}

type availableCouriersInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Limit int    `query:"limit" minimum:"1" maximum:"50" default:"20"`
}

type availableCouriersOutput struct {
	Body struct {
		Items []FreeCourierResponse `json:"items"`
	}
}

func (h handlers) availableCouriers(ctx context.Context, in *availableCouriersInput) (*availableCouriersOutput, error) {
	limit := in.Limit
	if limit == 0 {
		limit = 20
	}
	couriers, err := h.svc.AvailableCouriers(ctx, in.OrgID, limit)
	if err != nil {
		return nil, err
	}
	out := &availableCouriersOutput{}
	out.Body.Items = make([]FreeCourierResponse, len(couriers))
	for i, c := range couriers {
		out.Body.Items[i] = FreeCourierResponse{ID: c.ID, DisplayName: c.DisplayName}
	}
	return out, nil
}
