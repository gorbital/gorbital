package delivery

import (
	"context"
	"time"

	"example.com/shelfie/internal/modules/partners/domain"
)

// docs:start purchase-input

type purchaseInput struct {
	Body struct {
		EventID     string    `json:"event_id" maxLength:"100" doc:"The partner's own ID for this delivery; a repeat changes nothing" example:"evt_8xk2"`
		UserID      string    `json:"user_id" maxLength:"64" doc:"The reader's Shelfie account ID" example:"usr_2n4k"`
		ISBN        string    `json:"isbn" doc:"ISBN-13, with or without hyphens" example:"9780140449136"`
		Title       string    `json:"title" maxLength:"300" example:"The Odyssey"`
		PurchasedAt time.Time `json:"purchased_at"`
	}
}

func (h handlers) purchase(ctx context.Context, in *purchaseInput) (*purchaseOutput, error) {
	p, err := h.svc.RecordPurchase(ctx, h.partner, domain.Event{
		EventID:     in.Body.EventID,
		UserID:      in.Body.UserID,
		ISBN:        in.Body.ISBN,
		Title:       in.Body.Title,
		PurchasedAt: in.Body.PurchasedAt,
	})
	if err != nil {
		return nil, err
	}
	return toResponse(p), nil
}

// docs:end purchase-input
