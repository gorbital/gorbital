package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/usecase"
)

type listOrdersInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	page.Params
	Status string `query:"status" enum:"placed,accepted,preparing,ready,collected,delivered,rejected,cancelled" doc:"Only orders with this status"`
}

// OrderPage is a page of orders.
type OrderPage struct {
	Items      []OrderResponse `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type orderPageOutput struct {
	Body OrderPage
}

func (h handlers) listOrders(ctx context.Context, in *listOrdersInput) (*orderPageOutput, error) {
	res, err := h.svc.ListOrders(ctx, in.OrgID, usecase.ListOrdersInput{
		Page:   in.Params,
		Status: domain.Status(in.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &orderPageOutput{Body: OrderPage{Items: make([]OrderResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, order := range res.Items {
		out.Body.Items[i] = toResponse(order)
	}
	return out, nil
}
