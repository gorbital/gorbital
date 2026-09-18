package delivery

import "context"

type refundPaymentInput struct {
	ID   string `path:"id" maxLength:"64" example:"pay_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body struct {
		Reason string `json:"reason" minLength:"1" maxLength:"200" doc:"Why the money is being given back; the audit log keeps it"`
	}
}

func (h handlers) refundPayment(ctx context.Context, in *refundPaymentInput) (*paymentOutput, error) {
	payment, err := h.svc.RefundPayment(ctx, in.ID, in.Body.Reason)
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &paymentOutput{Body: toResponse(payment)}, nil
}
