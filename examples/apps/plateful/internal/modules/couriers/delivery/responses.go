package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/couriers/domain"
)

// CourierResponse is a courier's own profile, as the API returns it to the
// courier. It carries no organisation, because a courier has none.
type CourierResponse struct {
	ID          string `json:"id" example:"cur_mfrggzdfmztwq2lkmfrggzdfmy"`
	DisplayName string `json:"display_name"`
	Vehicle     string `json:"vehicle" enum:"bicycle,scooter,car,on_foot"`
	Available   bool   `json:"available" doc:"Whether you are on duty"`
	// ActiveOrderID is the order you are carrying, empty when you are free.
	// The orders module sets it.
	ActiveOrderID string    `json:"active_order_id,omitempty" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	Version       int64     `json:"version" example:"1" doc:"Increases with every change; send it back when updating"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type courierOutput struct {
	Body CourierResponse
}

func toResponse(c domain.Courier) CourierResponse {
	return CourierResponse{
		ID:            c.ID,
		DisplayName:   c.DisplayName,
		Vehicle:       string(c.Vehicle),
		Available:     c.Available,
		ActiveOrderID: c.ActiveOrderID,
		Version:       c.Version,
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}
}

// docs:start dispatch-response

// AvailableCourierResponse is a courier as a restaurant's dispatcher sees
// them: enough to choose one, and nothing else.
//
// This type exists so that the narrowing is impossible to forget. A
// restaurant reading this list is reading rows that are not its own, so the
// question is not "which rows" — the guard can't answer that here — but
// "which fields". The courier's user_id, their sign-in account, is not in
// it: it identifies a person across the whole platform, including in another
// restaurant's orders, and a dispatcher has no use for it. Neither is
// active_order_id, which would say which of their competitors' orders the
// courier is on.
type AvailableCourierResponse struct {
	ID          string `json:"id" example:"cur_mfrggzdfmztwq2lkmfrggzdfmy"`
	DisplayName string `json:"display_name"`
	Vehicle     string `json:"vehicle" enum:"bicycle,scooter,car,on_foot"`
}

// docs:end dispatch-response

type availableCouriersOutput struct {
	Body struct {
		Items []AvailableCourierResponse `json:"items"`
	}
}

func toAvailableResponse(couriers []domain.Courier) []AvailableCourierResponse {
	items := make([]AvailableCourierResponse, 0, len(couriers))
	for _, c := range couriers {
		items = append(items, AvailableCourierResponse{ID: c.ID, DisplayName: c.DisplayName, Vehicle: string(c.Vehicle)})
	}
	return items
}

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the courier is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}
