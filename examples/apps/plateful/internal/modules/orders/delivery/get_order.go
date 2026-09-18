package delivery

import "context"

func (h handlers) getOrder(ctx context.Context, in *orderIDInput) (*orderOutput, error) {
	order, err := h.svc.GetOrder(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &orderOutput{Body: toResponse(order)}, nil
}
