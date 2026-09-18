package delivery

import (
	"context"

	"example.com/plateful/internal/modules/payments/domain"
)

// docs:start payment-event-handler

// paymentEventInput is one delivery from the provider. Its whole body is
// covered by the signature guard.Webhook has already checked, so the id
// field can be trusted to be the provider's own event ID and not something
// a replayer chose.
//
// Both objects accept properties this app doesn't know: an event type it
// doesn't act on carries another shape entirely, and a provider adds fields
// without warning. Neither should be refused for failing this event's
// validation.
type paymentEventInput struct {
	Body struct {
		_    struct{}  `additionalProperties:"true"`
		ID   string    `json:"id" example:"evt_01J8Z" doc:"The provider's event ID. Applying an event already applied changes nothing."`
		Type string    `json:"type" example:"payment.authorised" doc:"payment.authorised, payment.captured or payment.failed; any other type is accepted and ignored"`
		Data eventData `json:"data" required:"false"`
	}
}

// eventData is what an event about a payment carries.
type eventData struct {
	_         struct{} `additionalProperties:"true"`
	PaymentID string   `json:"payment_id" required:"false" example:"pay_mfrggzdfmztwq2lkmfrggzdfmy"`
	Reason    string   `json:"reason" required:"false" doc:"Why the provider refused it, for a payment.failed event"`
}

type paymentEventOutput struct {
	Body struct {
		// Applied is false for a delivery already applied and for an event
		// type this app ignores: either way the provider should stop
		// retrying, so both answer 200.
		Applied   bool   `json:"applied"`
		PaymentID string `json:"payment_id,omitempty" example:"pay_mfrggzdfmztwq2lkmfrggzdfmy"`
		Status    string `json:"status,omitempty" enum:"pending,authorised,captured,refunded,failed"`
	}
}

// paymentEvent hands the delivery to the use case and says what became of
// it. The handler makes no decision of its own: which event types mean
// something, whether this one has been seen before, and whether the payment
// may move that way are three rules that belong to three different places,
// none of them here.
func (h handlers) paymentEvent(ctx context.Context, in *paymentEventInput) (*paymentEventOutput, error) {
	payment, applied, err := h.svc.ApplyEvent(ctx, domain.Event{
		ID:        in.Body.ID,
		Kind:      in.Body.Type,
		PaymentID: in.Body.Data.PaymentID,
		Reason:    in.Body.Data.Reason,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	out := &paymentEventOutput{}
	out.Body.Applied, out.Body.PaymentID, out.Body.Status = applied, payment.ID, string(payment.Status)
	return out, nil
}

// docs:end payment-event-handler
