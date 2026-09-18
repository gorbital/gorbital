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
	MaxAddressLength = 100
	MaxNoteLength    = 2000
)

// Status is one of the values an order's status can have.
type Status string

// Status values.
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

// Valid reports whether v is a known status.
func (v Status) Valid() bool {
	switch v {
	case StatusPlaced, StatusAccepted, StatusPreparing, StatusReady, StatusCollected, StatusDelivered, StatusRejected, StatusCancelled:
		return true
	}
	return false
}

// Order belongs to one organisation. Its members reach it through their
// role; nobody else can see or change it.
type Order struct {
	ID    string
	OrgID string
	// CreatedBy is the member who created it, a user or the organisation's
	// service account, for display and audit. Access comes only from
	// membership.
	CreatedBy string
	OrderFields
	// Version increases with every change. An update must name the version
	// it read, so it can't overwrite a change it hasn't seen.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// OrderFields are the fields users set.
type OrderFields struct {
	Status  Status
	Address string
	Note    string
}

// NewOrder returns a new order of orgID created by createdBy, or a
// *ValidationError. Text is trimmed, and an empty choice takes its first
// value.
func NewOrder(id, orgID, createdBy string, f OrderFields, now time.Time) (Order, error) {
	f.Address = strings.TrimSpace(f.Address)
	f.Note = strings.TrimSpace(f.Note)
	if f.Status == "" {
		f.Status = StatusPlaced
	}
	order := Order{ID: id, OrgID: orgID, CreatedBy: createdBy, OrderFields: f, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := order.validate(); err != nil {
		return Order{}, err
	}
	return order, nil
}

// Changes are the fields an update sets. Nil fields keep their value.
type Changes struct {
	Status  *Status
	Address *string
	Note    *string
}

// Apply returns order with c applied and the names of the fields whose value
// changed, or a *ValidationError. UpdatedAt becomes now only when a field
// changed. The repository increments the version when it saves.
func (order Order) Apply(c Changes, now time.Time) (Order, []string, error) {
	next := order
	var changed []string
	if c.Status != nil && *c.Status != order.Status {
		next.Status, changed = *c.Status, append(changed, "status")
	}
	if c.Address != nil {
		if value := strings.TrimSpace(*c.Address); value != order.Address {
			next.Address, changed = value, append(changed, "address")
		}
	}
	if c.Note != nil {
		if value := strings.TrimSpace(*c.Note); value != order.Note {
			next.Note, changed = value, append(changed, "note")
		}
	}
	if err := next.validate(); err != nil {
		return order, nil, err
	}
	if len(changed) > 0 {
		next.UpdatedAt = now
	}
	return next, changed, nil
}

func (order Order) validate() error {
	var errs []FieldError
	if !order.Status.Valid() {
		errs = append(errs, FieldError{Field: "status", Message: "must be placed, accepted, preparing, ready, collected, delivered, rejected or cancelled"})
	}
	if msg := checkText(order.Address, 1, MaxAddressLength); msg != "" {
		errs = append(errs, FieldError{Field: "address", Message: msg})
	}
	if msg := checkText(order.Note, 0, MaxNoteLength); msg != "" {
		errs = append(errs, FieldError{Field: "note", Message: msg})
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
