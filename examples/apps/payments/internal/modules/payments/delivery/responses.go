package delivery

import (
	"time"

	"example.com/payments/internal/modules/payments/domain"
)

// PaymentResponse is a recorded payment as the API returns it.
type PaymentResponse struct {
	ID string `json:"id" example:"pay_mfrggzdfmztwq2lkmfrggzdfmy"`
	// EventID is the provider's, so support can match a row to a delivery
	// in the provider's dashboard.
	EventID           string    `json:"event_id" example:"evt_2aBcDeFgHiJk"`
	ProviderPaymentID string    `json:"provider_payment_id" example:"pi_3QabcXYZ"`
	AmountMinor       int64     `json:"amount_minor" example:"4999" doc:"Minor units: 4999 is £49.99"`
	Currency          string    `json:"currency" example:"GBP"`
	Status            string    `json:"status" enum:"succeeded,refunded"`
	OccurredAt        time.Time `json:"occurred_at"`
	RecordedAt        time.Time `json:"recorded_at"`
}

func toResponse(p domain.Payment) PaymentResponse {
	return PaymentResponse{
		ID: p.ID, EventID: p.EventID, ProviderPaymentID: p.ProviderPaymentID,
		AmountMinor: p.AmountMinor, Currency: p.Currency, Status: string(p.Status),
		OccurredAt: p.OccurredAt, RecordedAt: p.RecordedAt,
	}
}
