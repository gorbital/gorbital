package delivery

import "context"

type getPaymentInput struct {
	ID string `path:"id" maxLength:"64" example:"pay_mfrggzdfmztwq2lkmfrggzdfmy"`
}

type paymentOutput struct {
	Body PaymentResponse
}

func (h handlers) getPayment(ctx context.Context, in *getPaymentInput) (*paymentOutput, error) {
	payment, err := h.svc.GetPayment(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &paymentOutput{Body: toResponse(payment)}, nil
}
