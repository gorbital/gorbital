package delivery

import (
	"context"
	"time"

	"example.com/plateful/internal/modules/orders/usecase"
)

// docs:start place-order-handler

type placeOrderInput struct {
	RestaurantID string `path:"restaurantId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The restaurant's ID, which is its organisation's"`
	Body         struct {
		Address      string     `json:"address,omitempty" maxLength:"200" doc:"Where to take it; the address on your profile by default"`
		Note         string     `json:"note,omitempty" maxLength:"500" doc:"Anything the kitchen or the courier should know"`
		ScheduledFor *time.Time `json:"scheduled_for,omitempty" doc:"When you want it; only while the orders.scheduled_ordering flag is on"`
		Items        []struct {
			ItemID   string `json:"item_id" minLength:"1" maxLength:"64"`
			Quantity int    `json:"quantity" minimum:"1" maximum:"99"`
		} `json:"items" minItems:"1" maxItems:"50"`
	}
}

// placeOrder is the whole handler: read the request, call the use case,
// shape the answer. It doesn't price anything, check anything or decide
// anything — the prices come from the menu inside the use case's
// transaction, which is the only place they can be read safely.
func (h handlers) placeOrder(ctx context.Context, in *placeOrderInput) (*orderOutput, error) {
	basket := usecase.PlaceOrderInput{Address: in.Body.Address, Note: in.Body.Note}
	if in.Body.ScheduledFor != nil {
		basket.ScheduledFor = *in.Body.ScheduledFor
	}
	for _, item := range in.Body.Items {
		basket.Items = append(basket.Items, usecase.BasketItem{ItemID: item.ItemID, Quantity: item.Quantity})
	}
	order, err := h.svc.PlaceOrder(ctx, in.RestaurantID, basket)
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &orderOutput{Body: toResponse(order)}, nil
}

// docs:end place-order-handler
