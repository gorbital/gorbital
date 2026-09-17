package usecase

import (
	"context"
	"time"

	"gorbital.dev/modules/mail/suppressionpg"

	"example.com/acme-api/internal/modules/mailevents/domain"
)

// A WebhookReader verifies a provider's signed webhook requests and reads
// their events. internal/app/infra_mail.go provides one for the provider
// chosen with `orb add mail`, or none.
type WebhookReader interface {
	// Provider names the provider, such as resend.
	Provider() string
	// Tolerance is how far a request's timestamp may be from now.
	Tolerance() time.Duration
	// Read returns the request's events. It returns an error wrapping
	// domain.ErrInvalidSignature for a request that isn't signed with the
	// secret within the tolerance of now, and domain.ErrInvalidPayload for
	// a signed body it can't read.
	Read(r domain.WebhookRequest, now time.Time) (domain.Webhook, error)
}

// SuppressionList adds addresses to the suppression list once per delivery.
// *suppressionpg.Store implements it.
type SuppressionList interface {
	AddOnce(ctx context.Context, key string, expiresAt time.Time, entries ...suppressionpg.Entry) ([]suppressionpg.Suppression, error)
}

var _ SuppressionList = (*suppressionpg.Store)(nil)
