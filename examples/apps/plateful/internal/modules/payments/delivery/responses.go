package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/payments/domain"
)

// PaymentResponse is a payment as the API returns it.
type PaymentResponse struct {
	ID      string `json:"id" example:"pay_mfrggzdfmztwq2lkmfrggzdfmy"`
	OrderID string `json:"order_id" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	// AmountMinor is money in integer minor units (pence), never a float,
	// so a client that reads it into a floating-point number is the one
	// introducing the rounding.
	AmountMinor int64  `json:"amount_minor" example:"2350" doc:"Integer minor units of currency, such as pence"`
	Currency    string `json:"currency" example:"GBP"`
	Status      string `json:"status" enum:"pending,authorised,captured,refunded,failed"`
	// ProviderRef is the provider's own identifier, which a customer quotes
	// to support and an operator finds in the provider's dashboard.
	ProviderRef   string    `json:"provider_ref" doc:"The payment provider's reference for this payment"`
	FailureReason string    `json:"failure_reason,omitempty" doc:"Why the provider refused it; absent unless the status is failed"`
	Version       int64     `json:"version" example:"1"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type paymentOutput struct {
	Body PaymentResponse
}

func toResponse(p domain.Payment) PaymentResponse {
	return PaymentResponse{
		ID:            p.ID,
		OrderID:       p.OrderID,
		AmountMinor:   p.AmountMinor,
		Currency:      p.Currency,
		Status:        string(p.Status),
		ProviderRef:   p.ProviderRef,
		FailureReason: p.FailureReason,
		Version:       p.Version,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the payment is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
