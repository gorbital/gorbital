package domain

import "errors"

// docs:start errors

// Errors of the payments use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrPaymentIDRequired reports an event without the provider's payment
	// ID.
	ErrPaymentIDRequired = errors.New("payments: the event needs a payment ID")
	// ErrInvalidAmount reports an amount that isn't a positive number of
	// minor units.
	ErrInvalidAmount = errors.New("payments: the amount must be positive, in minor units")
	// ErrInvalidCurrency reports a currency that isn't a three-letter code.
	ErrInvalidCurrency = errors.New("payments: the currency must be a three-letter code")
	// ErrPaymentNotFound reports a payment ID that isn't recorded.
	ErrPaymentNotFound = errors.New("payments: payment not found")
	// ErrDeliveryInProgress reports a delivery another transaction is
	// recording right now: the provider's own retry finishes the work, so
	// the answer asks it to send the delivery again.
	ErrDeliveryInProgress = errors.New("payments: the delivery is being recorded")
)

// docs:end errors
