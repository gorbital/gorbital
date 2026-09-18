// Package domain holds the payments module's payments and the rules about
// how money moves. It imports only the standard library, so the status
// machine below can be read, tested and changed without a database, an HTTP
// request or a payment provider anywhere near it.
package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Field limits. The migration's CHECK constraints match them.
const (
	CurrencyLength       = 3
	MaxProviderRefLength = 200
	MaxReasonLength      = 200
)

// docs:start payment-status-machine

// Status is where a payment is between the customer asking to pay and the
// money being settled or given back.
type Status string

// Statuses. A payment starts pending, because the app has asked the
// provider for the money and hasn't been told the answer yet; every other
// status arrives later, in an event the provider delivers.
const (
	StatusPending    Status = "pending"
	StatusAuthorised Status = "authorised"
	StatusCaptured   Status = "captured"
	StatusRefunded   Status = "refunded"
	StatusFailed     Status = "failed"
)

// transitions is the whole status machine, as a table rather than a chain
// of ifs in a handler: pending to authorised to captured, pending to
// failed, and authorised or captured to refunded. Refunded and failed have
// no entry at all, which is what makes them final.
//
// It lives here because more than one caller moves a payment — the
// provider's webhook and a platform refund — and a rule written in one
// handler is a rule the other handler doesn't have.
var transitions = map[Status][]Status{
	StatusPending:    {StatusAuthorised, StatusFailed},
	StatusAuthorised: {StatusCaptured, StatusRefunded},
	StatusCaptured:   {StatusRefunded},
}

// Valid reports whether v is a known status.
func (v Status) Valid() bool {
	if v == StatusRefunded || v == StatusFailed {
		return true
	}
	_, ok := transitions[v]
	return ok
}

// Final reports whether nothing moves out of v. A final payment is the end
// of the story: the money has been given back, or it never moved.
func (v Status) Final() bool {
	_, ok := transitions[v]
	return !ok
}

// CanMoveTo reports whether the machine has an arrow from v to next.
func (v Status) CanMoveTo(next Status) bool {
	for _, allowed := range transitions[v] {
		if allowed == next {
			return true
		}
	}
	return false
}

// docs:end payment-status-machine

// A Payment is what one customer owes for one order, and how far the money
// has got. There is at most one per order, which the migration's UNIQUE on
// order_id enforces.
type Payment struct {
	ID string
	// OrgID is the restaurant's organisation, copied from the order. The
	// customer belongs to no organisation, so this is the restaurant's side
	// of the row, and it is what an audit event and row-level security use.
	OrgID      string
	OrderID    string
	CustomerID string
	// AmountMinor is money in integer minor units (pence), never a float:
	// 0.1 has no exact binary representation, and what a customer is
	// charged must equal the order's total to the penny.
	AmountMinor int64
	Currency    string
	Status      Status
	// ProviderRef is the provider's own identifier for this payment, which
	// its events quote and its dashboard shows.
	ProviderRef string
	// FailureReason is why the provider refused it, empty otherwise.
	FailureReason string
	// Version increases with every change, so the webhook and a refund
	// can't overwrite each other.
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewPayment returns the pending payment of order orderID, or a
// *ValidationError. It is pending because the provider has been asked and
// hasn't answered: the answer arrives later, as an event.
func NewPayment(id, orgID, orderID, customerID string, amountMinor int64, currency, providerRef string, now time.Time) (Payment, error) {
	p := Payment{
		ID: id, OrgID: orgID, OrderID: orderID, CustomerID: customerID,
		AmountMinor: amountMinor, Currency: strings.ToUpper(strings.TrimSpace(currency)),
		Status: StatusPending, ProviderRef: strings.TrimSpace(providerRef),
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := p.validate(); err != nil {
		return Payment{}, err
	}
	return p, nil
}

// MoveTo returns the payment in status next, or the reason the machine
// refuses: ErrPaymentAlreadyFinal when it has already finished, and
// ErrPaymentNotPayable when there is simply no arrow from where it is to
// where the caller wants it. reason is kept only for a failure.
func (p Payment) MoveTo(next Status, reason string, now time.Time) (Payment, error) {
	switch {
	case !next.Valid():
		return p, &ValidationError{Errors: []FieldError{{Field: "status", Message: "must be a known payment status"}}}
	case p.Status.Final():
		return p, ErrPaymentAlreadyFinal
	case !p.Status.CanMoveTo(next):
		return p, ErrPaymentNotPayable
	}
	moved := p
	moved.Status, moved.UpdatedAt = next, now
	if next == StatusFailed {
		moved.FailureReason = trimTo(reason, MaxReasonLength)
	}
	return moved, nil
}

// docs:start payment-refund-rule

// Refund gives the money back. It is the same arrow the machine already
// has, with its own error: a customer whose card was never charged, and one
// whose refund has already been made, both hear that there is nothing to
// refund rather than that some status is wrong.
func (p Payment) Refund(now time.Time) (Payment, error) {
	if !p.Status.CanMoveTo(StatusRefunded) {
		return p, ErrPaymentNotRefundable
	}
	return p.MoveTo(StatusRefunded, "", now)
}

// docs:end payment-refund-rule

func (p Payment) validate() error {
	var errs []FieldError
	if p.OrderID == "" {
		errs = append(errs, FieldError{Field: "order_id", Message: "is required"})
	}
	if p.CustomerID == "" {
		errs = append(errs, FieldError{Field: "customer_id", Message: "is required"})
	}
	if p.AmountMinor < 0 {
		errs = append(errs, FieldError{Field: "amount_minor", Message: "must be zero or more minor units"})
	}
	if utf8.RuneCountInString(p.Currency) != CurrencyLength {
		errs = append(errs, FieldError{Field: "currency", Message: fmt.Sprintf("must be a %d-letter code, such as GBP", CurrencyLength)})
	}
	if utf8.RuneCountInString(p.ProviderRef) > MaxProviderRefLength {
		errs = append(errs, FieldError{Field: "provider_ref", Message: fmt.Sprintf("must be at most %d characters", MaxProviderRefLength)})
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// trimTo shortens s to at most maxLen characters, so a provider that sends
// a long explanation can't break the column's CHECK.
func trimTo(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	return string([]rune(s)[:maxLen])
}
