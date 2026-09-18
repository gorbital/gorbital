package delivery

import (
	"errors"
	"net/http"
	"time"

	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start order-response

// LineResponse is one dish on an order, as the API returns it.
type LineResponse struct {
	ItemID string `json:"item_id" doc:"The menu item it came from, for reference only"`
	// Name and PriceMinor are what the customer chose and was charged, taken
	// when the order was placed; the menu may have changed since.
	Name string `json:"name"`
	// PriceMinor is in integer minor units (pence). Money is never a
	// floating-point number: a total has to add up exactly.
	PriceMinor int64 `json:"price_minor" example:"1250"`
	Quantity   int   `json:"quantity" example:"2"`
}

// OrderResponse is an order as the API returns it. It is the same shape for
// a restaurant, a customer and a courier: which orders each may read is
// decided in the use cases, not by hiding fields.
type OrderResponse struct {
	ID           string         `json:"id" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	RestaurantID string         `json:"restaurant_id" example:"org_mfrggzdfmztwq2lkmfrggzdfmy" doc:"The restaurant, whose ID is its organisation's"`
	CustomerID   string         `json:"customer_id" example:"usr_mfrggzdfmztwq2lkmfrggzdfmy"`
	CourierID    string         `json:"courier_id,omitempty" example:"cur_mfrggzdfmztwq2lkmfrggzdfmy"`
	Status       string         `json:"status" enum:"placed,accepted,preparing,ready,collected,delivered,rejected,cancelled"`
	Address      string         `json:"address"`
	Note         string         `json:"note,omitempty"`
	Lines        []LineResponse `json:"lines"`
	TotalMinor   int64          `json:"total_minor" example:"2500" doc:"The sum of the lines, in minor units"`
	Currency     string         `json:"currency" example:"GBP"`
	ScheduledFor *time.Time     `json:"scheduled_for,omitempty"`
	PlacedAt     time.Time      `json:"placed_at"`
	AcceptedAt   *time.Time     `json:"accepted_at,omitempty"`
	ReadyAt      *time.Time     `json:"ready_at,omitempty"`
	CollectedAt  *time.Time     `json:"collected_at,omitempty"`
	DeliveredAt  *time.Time     `json:"delivered_at,omitempty"`
	ClosedAt     *time.Time     `json:"closed_at,omitempty"`
	ClosedReason string         `json:"closed_reason,omitempty"`
	Version      int64          `json:"version" example:"1"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// docs:end order-response

type orderOutput struct {
	Body OrderResponse
}

// orderIDInput is the path of the customer's and the courier's routes on one
// order. There is no organisation in it: neither caller has one.
type orderIDInput struct {
	ID string `path:"id" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
}

// orgOrderIDInput is the path of the restaurant's routes on one order.
type orgOrderIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func toResponse(o domain.Order) OrderResponse {
	res := OrderResponse{
		ID: o.ID, RestaurantID: o.OrgID, CustomerID: o.CustomerID, CourierID: o.CourierID,
		Status: string(o.Status), Address: o.Address, Note: o.Note,
		Lines: make([]LineResponse, len(o.Lines)), TotalMinor: o.TotalMinor, Currency: o.Currency,
		ScheduledFor: optional(o.ScheduledFor), PlacedAt: o.PlacedAt,
		AcceptedAt: optional(o.AcceptedAt), ReadyAt: optional(o.ReadyAt),
		CollectedAt: optional(o.CollectedAt), DeliveredAt: optional(o.DeliveredAt),
		ClosedAt: optional(o.ClosedAt), ClosedReason: o.ClosedReason,
		Version: o.Version, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
	for i, line := range o.Lines {
		res.Lines[i] = LineResponse{ItemID: line.ItemID, Name: line.Name, PriceMinor: line.PriceMinor, Quantity: line.Quantity}
	}
	return res
}

// optional returns a time to render, or nil for a step the order hasn't
// taken, so the JSON leaves it out rather than claiming the year 1.
func optional(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// docs:start field-errors

// fieldErrors lists invalid fields, found in location ("body" or "query");
// module.go maps the other errors.
func fieldErrors(err error, location string) error {
	var invalid *domain.ValidationError
	if !errors.As(err, &invalid) {
		return err
	}
	p := httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "the order is not valid")
	for _, f := range invalid.Errors {
		p.Errors = append(p.Errors, httpx.FieldError{Location: location + "." + f.Field, Message: f.Message})
	}
	return p
}

// docs:end field-errors
