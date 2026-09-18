package domain

import (
	"errors"
	"strings"
)

// Errors of the orders use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a member acting in
	// the organisation: a route without guard.OrgMember. An organisation the
	// caller isn't a member of never reaches the use cases; the guard
	// answers org_not_found.
	ErrUnauthenticated = errors.New("orders: a member of the organisation is required")

	// ErrInvalidOrder reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidOrder = errors.New("orders: invalid order")

	// ErrOrderNotFound reports an order that doesn't exist in the
	// organisation, such as another organisation's. The two aren't told
	// apart, so IDs can't be probed.
	ErrOrderNotFound = errors.New("orders: order not found")

	// ErrOrderVersionConflict reports an update to a version that is no
	// longer current.
	ErrOrderVersionConflict = errors.New("orders: order was changed since it was read")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of an order.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "orders: invalid order: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidOrder.
func (e *ValidationError) Unwrap() error { return ErrInvalidOrder }
