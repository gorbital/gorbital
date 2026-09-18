package domain

import (
	"strings"
	"time"
	"unicode/utf8"
)

// MaxEventIDLength bounds the provider's event ID, which is the primary key
// of payment_events.
const MaxEventIDLength = 200

// The event types this app acts on. A generic provider's vocabulary: one
// type per status the money can reach without anybody at the platform doing
// anything.
const (
	EventAuthorised = "payment.authorised"
	EventCaptured   = "payment.captured"
	EventFailed     = "payment.failed"
)

// An Event is one delivery from the provider, after guard.Webhook has
// checked its signature. ID is the provider's own identifier for the event,
// not for the delivery attempt: a retry of an event carries the ID again,
// which is what makes it possible to apply it once.
type Event struct {
	ID         string
	Kind       string
	PaymentID  string
	Reason     string
	ReceivedAt time.Time
}

// eventStatuses says which status each event type moves a payment to. An
// event type that isn't here is one this app doesn't act on.
var eventStatuses = map[string]Status{
	EventAuthorised: StatusAuthorised,
	EventCaptured:   StatusCaptured,
	EventFailed:     StatusFailed,
}

// StatusFor returns the status the event type moves a payment to, and
// whether the app acts on it at all. A provider's vocabulary grows without
// warning, and an event nobody here understands must be accepted and
// ignored rather than retried forever.
func StatusFor(kind string) (Status, bool) {
	s, ok := eventStatuses[kind]
	return s, ok
}

// Clean returns the event with its text trimmed and bounded, or a
// *ValidationError when it names no event or no payment. The provider signs
// what it sends; it doesn't promise the lengths this app's columns accept.
func (e Event) Clean() (Event, error) {
	e.ID, e.Kind = strings.TrimSpace(e.ID), strings.TrimSpace(e.Kind)
	e.PaymentID, e.Reason = strings.TrimSpace(e.PaymentID), trimTo(e.Reason, MaxReasonLength)
	var errs []FieldError
	switch {
	case e.ID == "":
		errs = append(errs, FieldError{Field: "id", Message: "is required"})
	case utf8.RuneCountInString(e.ID) > MaxEventIDLength:
		errs = append(errs, FieldError{Field: "id", Message: "is too long"})
	}
	if e.PaymentID == "" {
		errs = append(errs, FieldError{Field: "data.payment_id", Message: "is required"})
	}
	if len(errs) > 0 {
		return Event{}, &ValidationError{Errors: errs}
	}
	return e, nil
}
