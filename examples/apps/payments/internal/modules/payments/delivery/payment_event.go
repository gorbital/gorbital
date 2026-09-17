package delivery

import (
	"context"
	"time"

	"example.com/payments/internal/modules/payments/domain"
)

// docs:start event-input

// paymentEventInput is one delivery. DeliveryID is the provider's event ID
// from the signed webhook-id header, which guard.Webhook has already
// checked is present and part of the signature, so the body can't claim
// another event's identity.
//
// Everything under data is optional, and both objects accept properties
// this app doesn't know: an event type it doesn't record carries another
// shape entirely, and a provider adds fields without warning. Neither
// should be refused for failing this event's validation.
type paymentEventInput struct {
	DeliveryID string `header:"webhook-id" doc:"The provider's event ID, signed with the body"`
	Body       struct {
		_    struct{}  `additionalProperties:"true"`
		Type string    `json:"type" example:"payment.succeeded" doc:"The provider's event type"`
		Data eventData `json:"data" required:"false"`
	}
}

// eventData is what a payment.succeeded or payment.refunded event carries.
type eventData struct {
	_           struct{}  `additionalProperties:"true"`
	PaymentID   string    `json:"payment_id" required:"false" example:"pi_3QabcXYZ"`
	AmountMinor int64     `json:"amount_minor" required:"false" example:"4999" doc:"Minor units, positive"`
	Currency    string    `json:"currency" required:"false" example:"GBP"`
	OccurredAt  time.Time `json:"occurred_at" required:"false"`
}

// docs:end event-input

type paymentEventOutput struct {
	Body struct {
		// Applied is false for a delivery already recorded and for an event
		// type this app ignores: either way the provider should stop
		// retrying, so both answer 200.
		Applied   bool   `json:"applied"`
		PaymentID string `json:"payment_id,omitempty" example:"pay_mfrggzdfmztwq2lkmfrggzdfmy"`
	}
}

// docs:start event-handler

func (h handlers) paymentEvent(ctx context.Context, in *paymentEventInput) (*paymentEventOutput, error) {
	payment, applied, err := h.svc.RecordEvent(ctx, domain.Event{
		ID:          in.DeliveryID,
		Type:        in.Body.Type,
		PaymentID:   in.Body.Data.PaymentID,
		AmountMinor: in.Body.Data.AmountMinor,
		Currency:    in.Body.Data.Currency,
		OccurredAt:  in.Body.Data.OccurredAt,
	})
	if err != nil {
		return nil, err
	}
	out := &paymentEventOutput{}
	out.Body.Applied, out.Body.PaymentID = applied, payment.ID
	return out, nil
}

// docs:end event-handler
