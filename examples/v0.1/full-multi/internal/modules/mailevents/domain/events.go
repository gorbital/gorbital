// Package domain holds the mail events module's events, rules and errors.
// It imports only the standard library.
package domain

import "errors"

// A Kind is what happened to a sent email.
type Kind string

// Kinds of delivery events.
const (
	// KindBounce is a bounce: permanent (hard) or temporary (soft).
	KindBounce Kind = "bounce"
	// KindComplaint is a recipient marking the email as spam.
	KindComplaint Kind = "complaint"
	// KindOther is any other event, such as a delivery; it changes nothing.
	KindOther Kind = "other"
)

// An Event is one delivery event a provider reported for an email.
type Event struct {
	Kind Kind
	// Permanent reports a hard bounce.
	Permanent bool
	// Recipients are the email's recipients: personal data, never logged.
	Recipients []string
	// Detail is the provider's classification, such as Permanent/General;
	// never a server's message, which can quote addresses.
	Detail string
}

// Suppresses reports whether the event puts its recipients on the
// suppression list: hard bounces and complaints do; soft bounces, which
// retrying can fix, and other events don't.
func (e Event) Suppresses() bool {
	return e.Kind == KindComplaint || (e.Kind == KindBounce && e.Permanent)
}

// A WebhookRequest is a provider's webhook request as received, following
// the Standard Webhooks shape Svix uses: a delivery ID, a timestamp and
// signatures in headers, and the raw body they sign.
type WebhookRequest struct {
	ID        string
	Timestamp string
	Signature string
	Body      []byte
}

// A Webhook is a verified webhook request.
type Webhook struct {
	// ID is the provider's delivery ID, the same for every retry.
	ID     string
	Events []Event
}

// Errors returned by the mail events use cases.
var (
	// ErrWebhookNotConfigured reports a webhook for a provider the app
	// doesn't use, or whose signing secret isn't set.
	ErrWebhookNotConfigured = errors.New("webhook not configured")
	// ErrInvalidSignature reports a request whose signature, timestamp or
	// headers aren't valid.
	ErrInvalidSignature = errors.New("invalid webhook signature")
	// ErrInvalidPayload reports a correctly signed body that isn't an event.
	ErrInvalidPayload = errors.New("invalid webhook payload")
)
