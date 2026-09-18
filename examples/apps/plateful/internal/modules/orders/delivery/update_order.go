package delivery

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/usecase"
)

type updateOrderInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Version int64   `json:"version" minimum:"1" doc:"The version you read. If the order changed since, the update fails with order_version_conflict."`
		Status  *string `json:"status,omitempty" enum:"placed,accepted,preparing,ready,collected,delivered,rejected,cancelled"`
		Address *string `json:"address,omitempty" minLength:"1" maxLength:"100"`
		Note    *string `json:"note,omitempty" maxLength:"2000"`
	}
}

func (h handlers) updateOrder(ctx context.Context, in *updateOrderInput) (*orderOutput, error) {
	changes := domain.Changes{
		Address: in.Body.Address,
		Note:    in.Body.Note,
	}
	if in.Body.Status != nil {
		value := domain.Status(*in.Body.Status)
		changes.Status = &value
	}
	order, err := h.svc.UpdateOrder(ctx, in.OrgID, in.ID, usecase.UpdateOrderInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &orderOutput{Body: toResponse(order)}, nil
}
