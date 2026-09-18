package domain

import (
	"errors"
	"strings"
)

// Errors of the payments use cases. module.go maps each to an HTTP status
// and a problem code, which are public API. invalid_webhook_signature isn't
// among them: guard.Webhook answers that itself, before a handler runs.
var (
	// ErrUnauthenticated reports an operation with no signed-in account
	// behind it. The customer routes are guarded by guard.Permission, which
	// refuses an anonymous caller first, so this is the use case's own
	// belt-and-braces check for a caller reaching it another way.
	ErrUnauthenticated = errors.New("payments: a signed-in account is required")

	// ErrInvalidPayment reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidPayment = errors.New("payments: invalid payment")

	// ErrPaymentNotFound reports a payment that doesn't exist, or that the
	// caller may not see. The cases aren't told apart, so IDs can't be
	// probed.
	ErrPaymentNotFound = errors.New("payments: payment not found")

	// ErrOrderNotFound reports an order that doesn't exist, or that belongs
	// to another customer. The two look the same on purpose: a customer
	// must not learn which order IDs exist by paying for them.
	ErrOrderNotFound = errors.New("payments: order not found")

	// ErrPaymentNotPayable reports money that can't move the way it was
	// asked to: an order that isn't waiting to be paid, an order whose
	// payment has already gone past pending, or a status change the machine
	// in payment.go has no arrow for. One error for the two because they
	// are one answer to the customer — this order can't be paid now — and
	// they map to the same problem code.
	ErrPaymentNotPayable = errors.New("payments: the order can't be paid in its current state")

	// ErrPaymentAlreadyFinal reports a change to a payment that has already
	// finished, refunded or failed. Nothing moves out of a final status,
	// whichever event arrives.
	ErrPaymentAlreadyFinal = errors.New("payments: the payment has already reached a final status")

	// ErrPaymentNotRefundable reports a refund of a payment that never took
	// any money, or whose money has already been given back.
	ErrPaymentNotRefundable = errors.New("payments: the payment can't be refunded")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a payment.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "payments: invalid payment: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidPayment.
func (e *ValidationError) Unwrap() error { return ErrInvalidPayment }
