package domain

import (
	"errors"
	"strings"
)

// Errors of the orders use cases. module.go maps each to an HTTP status and
// a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a signed-in caller.
	ErrUnauthenticated = errors.New("orders: a signed-in caller is required")

	// ErrInvalidOrder reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidOrder = errors.New("orders: invalid order")

	// docs:start not-found-error
	// ErrOrderNotFound reports an order that doesn't exist, or that this
	// caller has nothing to do with: another restaurant's, somebody else's,
	// or one assigned to a different courier. The cases are deliberately
	// not told apart, so an ID can't be probed to learn that an order
	// exists.
	ErrOrderNotFound = errors.New("orders: order not found")
	// docs:end not-found-error

	// ErrInvalidTransition reports a status change the state machine
	// doesn't allow, such as accepting an order that was already rejected.
	ErrInvalidTransition = errors.New("orders: the order can't move to that status")

	// ErrRestaurantNotFound reports a restaurant that doesn't exist, or that
	// isn't open to customers.
	ErrRestaurantNotFound = errors.New("orders: restaurant not found")

	// ErrRestaurantNotAccepting reports an order placed at a restaurant that
	// isn't taking any: onboarding, paused, suspended, or outside its hours.
	ErrRestaurantNotAccepting = errors.New("orders: the restaurant is not taking orders")

	// ErrRestaurantBusy reports a restaurant with as many open orders as the
	// orders.max_open_per_restaurant setting allows.
	ErrRestaurantBusy = errors.New("orders: the restaurant has as many open orders as allowed")

	// ErrItemUnavailable reports a dish that isn't on the restaurant's menu
	// or isn't available today.
	ErrItemUnavailable = errors.New("orders: a dish is not available")

	// ErrItemOutOfStock reports a dish with limited stock and not enough of
	// it left.
	ErrItemOutOfStock = errors.New("orders: a dish has run out")

	// docs:start payment-error
	// ErrPaymentNotAuthorised reports accepting an order whose payment
	// hasn't been authorised. The rule spans two modules: the status lives
	// in the payments module's order_payments table, and the refusal is
	// here, where accepting happens.
	ErrPaymentNotAuthorised = errors.New("orders: the order's payment is not authorised")
	// docs:end payment-error

	// ErrCourierUnavailable reports assigning a courier who isn't available
	// or is already carrying another order.
	ErrCourierUnavailable = errors.New("orders: the courier is not available")

	// ErrNotACourier reports a courier route used by an account with no
	// courier profile.
	ErrNotACourier = errors.New("orders: the caller is not a courier")

	// ErrSchedulingUnavailable reports an order scheduled for later while
	// the orders.scheduled_ordering flag is off for this caller.
	ErrSchedulingUnavailable = errors.New("orders: scheduled ordering is not available")
)

// docs:start validation-error

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

// docs:end validation-error
