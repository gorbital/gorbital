package delivery

import (
	"time"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

// TripResponse is a trip as the API returns it.
type TripResponse struct {
	ID          string    `json:"id" example:"trp_mfrggzdfmztwq2lkmfrggzdfmy"`
	Destination string    `json:"destination" example:"Lisbon"`
	Notes       string    `json:"notes" example:"Three nights in Alfama."`
	CreatedAt   time.Time `json:"created_at"`
}

type tripOutput struct {
	Body TripResponse
}

// tripIDInput is the path of the routes on one trip.
type tripIDInput struct {
	ID string `path:"id" maxLength:"64" example:"trp_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(t domain.Trip) TripResponse {
	return TripResponse{ID: t.ID, Destination: t.Destination, Notes: t.Notes, CreatedAt: t.CreatedAt}
}
