package delivery

import "context"

// docs:start pay-order-handler

type orderIDInput struct {
	OrderID string `path:"orderId" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
}

// payOrder is the whole handler: read the request, call the use case, shape
// the answer. There is nothing about ownership here, and that is the point
// — the caller is a customer in no organisation, so the question "is this
// order yours?" is a row in the database, not a guard, and it is answered
// in usecase.PayOrder where the row is read.
//
// Nothing about the Idempotency-Key header is here either. gorbital's
// middleware stack already honours it on every POST, stores the first
// response and replays it with Idempotent-Replayed: true; a module that
// built its own would be building a second one.
func (h handlers) payOrder(ctx context.Context, in *orderIDInput) (*paymentOutput, error) {
	payment, _, err := h.svc.PayOrder(ctx, in.OrderID)
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &paymentOutput{Body: toResponse(payment)}, nil
}

// docs:end pay-order-handler
