package delivery

import "context"

// getOrderPayment answers the customer's own "has it gone through yet?".
// The status it returns is the one the restaurant's own module reads before
// it lets the order be accepted.
func (h handlers) getOrderPayment(ctx context.Context, in *orderIDInput) (*paymentOutput, error) {
	payment, err := h.svc.GetOrderPayment(ctx, in.OrderID)
	if err != nil {
		return nil, err
	}
	return &paymentOutput{Body: toResponse(payment)}, nil
}
