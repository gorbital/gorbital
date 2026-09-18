package domain

import (
	"errors"
	"strings"
)

// Errors of the restaurants use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a member acting in
	// the organisation: a route without guard.OrgMember. An organisation the
	// caller isn't a member of never reaches the use cases; the guard
	// answers org_not_found.
	ErrUnauthenticated = errors.New("restaurants: a member of the organisation is required")

	// ErrInvalidRestaurant reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidRestaurant = errors.New("restaurants: invalid restaurant")

	// ErrRestaurantNotFound reports a restaurant that doesn't exist, or that
	// the caller may not see: an organisation without a profile yet, or a
	// restaurant that isn't open, asked for by a customer. The cases aren't
	// told apart, so IDs can't be probed.
	ErrRestaurantNotFound = errors.New("restaurants: restaurant not found")

	// ErrRestaurantNameTaken reports a name another restaurant on the
	// platform already uses, ignoring case.
	ErrRestaurantNameTaken = errors.New("restaurants: restaurant name is already taken")

	// ErrRestaurantVersionConflict reports an update to a version that is no
	// longer current.
	ErrRestaurantVersionConflict = errors.New("restaurants: restaurant was changed since it was read")

	// ErrRestaurantSuspended reports a change to a restaurant platform staff
	// suspended, or a second suspension of one.
	ErrRestaurantSuspended = errors.New("restaurants: the restaurant is suspended")

	// ErrRestaurantNotSuspended reports lifting a suspension a restaurant
	// doesn't have.
	ErrRestaurantNotSuspended = errors.New("restaurants: the restaurant is not suspended")

	// ErrSuspensionIsPlatformOnly reports a restaurant's own staff setting
	// the suspended status. Suspension is platform staff's
	// (usecase.PermSuspend).
	ErrSuspensionIsPlatformOnly = errors.New("restaurants: only platform staff suspend a restaurant")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a restaurant.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "restaurants: invalid restaurant: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidRestaurant.
func (e *ValidationError) Unwrap() error { return ErrInvalidRestaurant }
