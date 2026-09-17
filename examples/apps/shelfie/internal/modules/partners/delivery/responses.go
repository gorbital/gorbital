package delivery

import (
	"time"

	"example.com/shelfie/internal/modules/partners/domain"
)

// PurchaseResponse is a partner purchase as the API returns it.
type PurchaseResponse struct {
	ID          string    `json:"id" example:"prc_k3n4..."`
	Partner     string    `json:"partner" example:"pagebound"`
	ISBN        string    `json:"isbn" example:"9780140449136"`
	Title       string    `json:"title" example:"The Odyssey"`
	PurchasedAt time.Time `json:"purchased_at"`
}

type purchaseOutput struct {
	Body PurchaseResponse
}

func toResponse(p domain.Purchase) *purchaseOutput {
	return &purchaseOutput{Body: PurchaseResponse{
		ID: p.ID, Partner: p.Partner, ISBN: p.ISBN, Title: p.Title, PurchasedAt: p.PurchasedAt,
	}}
}
