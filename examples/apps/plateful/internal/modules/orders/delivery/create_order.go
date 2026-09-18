package delivery

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

type createOrderInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Status  string `json:"status,omitempty" enum:"placed,accepted,preparing,ready,collected,delivered,rejected,cancelled" default:"placed"`
		Address string `json:"address" minLength:"1" maxLength:"100"`
		Note    string `json:"note,omitempty" maxLength:"2000"`
	}
}

func (h handlers) createOrder(ctx context.Context, in *createOrderInput) (*orderOutput, error) {
	order, err := h.svc.CreateOrder(ctx, in.OrgID, domain.OrderFields{
		Status:  domain.Status(in.Body.Status),
		Address: in.Body.Address,
		Note:    in.Body.Note,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &orderOutput{Body: toResponse(order)}, nil
}
