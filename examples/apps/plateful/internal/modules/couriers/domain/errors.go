package domain

import (
	"errors"
	"strings"
)

// Errors of the couriers use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a signed-in account.
	// A courier's routes carry no organisation, so there is no membership to
	// fall back on: either the request has an account or it has nothing.
	ErrUnauthenticated = errors.New("couriers: a signed-in account is required")

	// ErrInvalidCourier reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidCourier = errors.New("couriers: invalid courier")

	// ErrCourierNotFound reports a courier profile that doesn't exist, or
	// that the caller may not see: somebody else's. The two cases aren't
	// told apart, so a courier can't be found by probing accounts.
	ErrCourierNotFound = errors.New("couriers: courier not found")

	// ErrCourierAlreadyRegistered reports a second profile for one account.
	// A person delivers as themselves; two profiles would be two identities
	// for the same sign-in.
	ErrCourierAlreadyRegistered = errors.New("couriers: this account is already registered as a courier")

	// ErrCourierOnDelivery reports a courier going off duty while an order
	// is on them. The orders module releases them when the delivery ends.
	ErrCourierOnDelivery = errors.New("couriers: the courier is carrying an order")

	// ErrCourierVersionConflict reports an update to a version that is no
	// longer current.
	ErrCourierVersionConflict = errors.New("couriers: courier was changed since it was read")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a courier.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "couriers: invalid courier: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidCourier.
func (e *ValidationError) Unwrap() error { return ErrInvalidCourier }
