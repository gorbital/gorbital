package delivery

import "context"

func (h handlers) getOrder(ctx context.Context, in *orderIDInput) (*orderOutput, error) {
	order, err := h.svc.GetOrder(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &orderOutput{Body: toResponse(order)}, nil
}

func (h handlers) getOrgOrder(ctx context.Context, in *orgOrderIDInput) (*orderOutput, error) {
	order, err := h.svc.GetOrgOrder(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &orderOutput{Body: toResponse(order)}, nil
}
