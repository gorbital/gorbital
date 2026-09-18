package delivery

import "context"

type listAvailableCouriersInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Limit int    `query:"limit" minimum:"1" maximum:"100" default:"20" doc:"How many couriers to return at most"`
}

// listAvailableCouriers returns the couriers a restaurant could send an
// order out with. orgId is the caller's restaurant, which guard.OrgMember
// has already checked; it never reaches the query, because the couriers it
// returns belong to no organisation. The response is narrowed to what a
// dispatcher needs (AvailableCourierResponse).
func (h handlers) listAvailableCouriers(ctx context.Context, in *listAvailableCouriersInput) (*availableCouriersOutput, error) {
	couriers, err := h.svc.ListAvailableCouriers(ctx, in.OrgID, in.Limit)
	if err != nil {
		return nil, err
	}
	out := &availableCouriersOutput{}
	out.Body.Items = toAvailableResponse(couriers)
	return out, nil
}
