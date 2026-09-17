// Package domain holds the billing API's payments and their rules. It
// imports only the standard library.
package domain

import (
	"strings"
	"time"
)

// docs:start event-types

// The event types the provider sends that this app records. Any other type
// is accepted and ignored, so the provider stops retrying it and adding a
// handler later is a deploy, not a backfill.
const (
	EventSucceeded = "payment.succeeded"
	EventRefunded  = "payment.refunded"
)

// A Status says which way the money moved.
type Status string

// The statuses, one per recorded event type.
const (
	StatusSucceeded Status = "succeeded"
	StatusRefunded  Status = "refunded"
)

// StatusFor returns the status the event type records, and false for a type
// this app doesn't act on.
func StatusFor(eventType string) (Status, bool) {
	switch eventType {
	case EventSucceeded:
		return StatusSucceeded, true
	case EventRefunded:
		return StatusRefunded, true
	default:
		return "", false
	}
}

// docs:end event-types

// An Event is what one webhook delivery says happened. ID is the provider's
// delivery ID, which identifies the event and not the request: every retry
// of one event repeats it.
type Event struct {
	ID          string
	Type        string
	PaymentID   string
	AmountMinor int64
	Currency    string
	OccurredAt  time.Time
}

// A Payment is one settled movement of money, recorded from one delivery.
type Payment struct {
	// ID is ours, "pay_" and 128 random bits; EventID is the provider's.
	ID      string
	EventID string
	// ProviderPaymentID is the payment the provider's event is about.
	// Several payments share it: one per event, such as a payment and its
	// refund.
	ProviderPaymentID string
	// AmountMinor is in minor units and always positive: 4999 is £49.99.
	AmountMinor int64
	Currency    string
	Status      Status
	OccurredAt  time.Time
	RecordedAt  time.Time
}

// docs:start new-payment

// NewPayment returns the payment that event e records with status, or
// ErrPaymentIDRequired, ErrInvalidAmount or ErrInvalidCurrency. A delivery
// is signed, so its body comes from the provider; that doesn't make it
// well formed, and a provider that sends an amount of 0 or -1 must not
// leave a row that says money moved.
func NewPayment(id string, status Status, e Event, now time.Time) (Payment, error) {
	p := Payment{
		ID: id, EventID: e.ID, ProviderPaymentID: strings.TrimSpace(e.PaymentID),
		AmountMinor: e.AmountMinor, Currency: strings.ToUpper(strings.TrimSpace(e.Currency)),
		Status: status, OccurredAt: e.OccurredAt.UTC(), RecordedAt: now,
	}
	if p.OccurredAt.IsZero() {
		p.OccurredAt = now
	}
	switch {
	case p.ProviderPaymentID == "":
		return Payment{}, ErrPaymentIDRequired
	case p.AmountMinor <= 0:
		return Payment{}, ErrInvalidAmount
	case !validCurrency(p.Currency):
		return Payment{}, ErrInvalidCurrency
	}
	return p, nil
}

// validCurrency reports whether c is three ASCII letters, as ISO 4217 codes
// are. The list of codes changes; its shape doesn't.
func validCurrency(c string) bool {
	if len(c) != 3 {
		return false
	}
	for i := range len(c) {
		if c[i] < 'A' || c[i] > 'Z' {
			return false
		}
	}
	return true
}

// docs:end new-payment
