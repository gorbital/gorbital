// Package domain holds the orders module's orders and their rules. It
// imports only the standard library.
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits. The migration's CHECK constraints match them.
const (
	MaxAddressLength  = 200
	MaxNoteLength     = 500
	MaxReasonLength   = 500
	MaxItemNameLength = 100
	// MaxLines is how many different dishes one order may carry, and
	// MaxQuantity how many of each.
	MaxLines    = 50
	MaxQuantity = 99
)

// docs:start order-status

// Status is where an order is. The happy path runs placed → accepted →
// preparing → ready → collected → delivered; rejected and cancelled are the
// two ways it ends early. Status values are public API: clients switch on
// them.
type Status string

// Statuses.
const (
	StatusPlaced    Status = "placed"
	StatusAccepted  Status = "accepted"
	StatusPreparing Status = "preparing"
	StatusReady     Status = "ready"
	StatusCollected Status = "collected"
	StatusDelivered Status = "delivered"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
)

// transitions is the state machine: from each status, the statuses an order
// may move to. Anything missing from this table is refused with
// ErrInvalidTransition, so the machine is one readable thing rather than a
// condition spread over six handlers. A status with no entry is terminal.
var transitions = map[Status][]Status{
	StatusPlaced:    {StatusAccepted, StatusRejected, StatusCancelled},
	StatusAccepted:  {StatusPreparing, StatusCancelled},
	StatusPreparing: {StatusReady, StatusCancelled},
	StatusReady:     {StatusCollected},
	StatusCollected: {StatusDelivered},
}

// Valid reports whether v is a known status.
func (v Status) Valid() bool {
	switch v {
	case StatusPlaced, StatusAccepted, StatusPreparing, StatusReady,
		StatusCollected, StatusDelivered, StatusRejected, StatusCancelled:
		return true
	}
	return false
}

// Terminal reports whether nothing more can happen to an order in this
// status.
func (v Status) Terminal() bool { return len(transitions[v]) == 0 }

// CanMoveTo reports whether an order in this status may move to next.
func (v Status) CanMoveTo(next Status) bool {
	for _, allowed := range transitions[v] {
		if allowed == next {
			return true
		}
	}
	return false
}

// docs:end order-status

// docs:start order-line

// A Line is one dish on an order.
type Line struct {
	// ItemID names the menu item it came from, for the restaurant's
	// reporting. It is not a link: the item may be renamed or deleted.
	ItemID string
	// Name and PriceMinor are a snapshot taken when the order was placed.
	// They are what the customer chose and was charged, and a menu edited
	// tomorrow must not rewrite either. This is why the order doesn't join
	// to menu_items to render itself.
	Name string
	// PriceMinor is the price of one, in integer minor units (pence): money
	// is never a float, because 0.1 has no exact binary representation and
	// a total must equal the sum of its lines to the penny.
	PriceMinor int64
	Quantity   int
}

// Total returns what the line costs, in minor units.
func (l Line) Total() int64 { return l.PriceMinor * int64(l.Quantity) }

// docs:end order-line

// An Order is one customer's order from one restaurant.
type Order struct {
	ID string
	// OrgID is the restaurant the order is from. A restaurant is an
	// organisation, so its staff reach the order through this one ID.
	OrgID string
	// CustomerID is the account that placed it. It is a member of no
	// organisation, so nothing about membership decides what it may do; the
	// use cases compare this against the caller.
	CustomerID string
	// CourierID is the courier carrying it, empty while none is assigned.
	// Couriers are platform-scoped too.
	CourierID string
	Status    Status
	Address   string
	Note      string
	// ScheduledFor is when the customer wants it, zero for as soon as
	// possible.
	ScheduledFor time.Time
	Lines        []Line
	// TotalMinor is the sum of the lines in minor units, computed when the
	// order is placed and never recomputed from the menu afterwards.
	TotalMinor int64
	Currency   string

	PlacedAt     time.Time
	AcceptedAt   time.Time
	ReadyAt      time.Time
	CollectedAt  time.Time
	DeliveredAt  time.Time
	ClosedAt     time.Time
	ClosedReason string

	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Basket is what a customer sends: the dishes, where to deliver, and when.
type Basket struct {
	Address      string
	Note         string
	ScheduledFor time.Time
	Lines        []Line
}

// NewOrder returns a placed order, or a *ValidationError. The lines are
// already priced from the menu by the use case; this checks what an order
// must look like and adds up the total.
func NewOrder(id, orgID, customerID, currency string, b Basket, now time.Time) (Order, error) {
	o := Order{
		ID: id, OrgID: orgID, CustomerID: customerID,
		Status: StatusPlaced, Address: strings.TrimSpace(b.Address), Note: strings.TrimSpace(b.Note),
		ScheduledFor: b.ScheduledFor, Lines: b.Lines, Currency: currency,
		PlacedAt: now, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	for _, line := range o.Lines {
		o.TotalMinor += line.Total()
	}
	if err := o.validate(); err != nil {
		return Order{}, err
	}
	return o, nil
}

// docs:start order-move

// MoveTo returns the order in status next, or ErrInvalidTransition when the
// machine doesn't allow the move. It stamps the time of the step it took, so
// an order always carries when each thing happened, and closes the order
// when it reaches a terminal status.
//
// This is where every status change goes: accepting, rejecting, a courier
// collecting, a customer cancelling. A handler decides who is asking; the
// order decides whether what they ask is possible.
func (o Order) MoveTo(next Status, reason string, now time.Time) (Order, error) {
	if !next.Valid() {
		return o, ErrInvalidTransition
	}
	if !o.Status.CanMoveTo(next) {
		return o, ErrInvalidTransition
	}
	moved := o
	moved.Status, moved.UpdatedAt = next, now
	switch next {
	case StatusAccepted:
		moved.AcceptedAt = now
	case StatusReady:
		moved.ReadyAt = now
	case StatusCollected:
		moved.CollectedAt = now
	case StatusDelivered:
		moved.DeliveredAt = now
	}
	if next.Terminal() {
		moved.ClosedAt = now
		moved.ClosedReason = strings.TrimSpace(reason)
		if msg := checkText(moved.ClosedReason, 0, MaxReasonLength); msg != "" {
			return o, &ValidationError{Errors: []FieldError{{Field: "reason", Message: msg}}}
		}
	}
	return moved, nil
}

// docs:end order-move

// AssignCourier gives the order to courierID. An order that is finished, or
// that a courier has already collected, can't change hands.
func (o Order) AssignCourier(courierID string, now time.Time) (Order, error) {
	switch {
	case o.Status.Terminal() || o.Status == StatusCollected:
		return o, ErrInvalidTransition
	case o.CourierID == courierID:
		return o, nil // already theirs: assigning again changes nothing
	}
	next := o
	next.CourierID, next.UpdatedAt = courierID, now
	return next, nil
}

// Late reports whether the restaurant has had the order longer than after
// without delivering it.
func (o Order) Late(after time.Duration, now time.Time) bool {
	return !o.Status.Terminal() && !o.AcceptedAt.IsZero() && now.Sub(o.AcceptedAt) > after
}

func (o Order) validate() error {
	var errs []FieldError
	if msg := checkText(o.Address, 1, MaxAddressLength); msg != "" {
		errs = append(errs, FieldError{Field: "address", Message: msg})
	}
	if msg := checkText(o.Note, 0, MaxNoteLength); msg != "" {
		errs = append(errs, FieldError{Field: "note", Message: msg})
	}
	switch {
	case len(o.Lines) == 0:
		errs = append(errs, FieldError{Field: "items", Message: "an order needs at least one dish"})
	case len(o.Lines) > MaxLines:
		errs = append(errs, FieldError{Field: "items", Message: fmt.Sprintf("must be at most %d different dishes", MaxLines)})
	}
	for i, line := range o.Lines {
		field := fmt.Sprintf("items.%d", i)
		switch {
		case line.Quantity < 1 || line.Quantity > MaxQuantity:
			errs = append(errs, FieldError{Field: field + ".quantity", Message: fmt.Sprintf("must be between 1 and %d", MaxQuantity)})
		case line.PriceMinor < 0:
			errs = append(errs, FieldError{Field: field + ".price", Message: "must not be negative"})
		}
		if msg := checkText(line.Name, 1, MaxItemNameLength); msg != "" {
			errs = append(errs, FieldError{Field: field + ".name", Message: msg})
		}
	}
	if len(o.Currency) != 3 {
		errs = append(errs, FieldError{Field: "currency", Message: "must be a three-letter code, such as GBP"})
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// checkText returns why s isn't a valid text of min to max characters, or "".
func checkText(s string, minLen, maxLen int) string {
	switch n := utf8.RuneCountInString(s); {
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return "must be valid text"
	case n < minLen:
		return "is required"
	case n > maxLen:
		return fmt.Sprintf("must be at most %d characters", maxLen)
	}
	return ""
}
