package delivery

import (
	"context"
)

type purchaseListOutput struct {
	Body struct {
		Items []PurchaseResponse `json:"items"`
	}
}

func (h handlers) listPurchases(ctx context.Context, _ *struct{}) (*purchaseListOutput, error) {
	list, err := h.svc.ListPurchases(ctx)
	if err != nil {
		return nil, err
	}
	out := &purchaseListOutput{}
	out.Body.Items = make([]PurchaseResponse, 0, len(list))
	for _, p := range list {
		out.Body.Items = append(out.Body.Items, toResponse(p).Body)
	}
	return out, nil
}
