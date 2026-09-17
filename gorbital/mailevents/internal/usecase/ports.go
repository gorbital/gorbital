package usecase

import (
	"context"
	"time"

	"gorbital.dev/modules/mail/suppressionpg"

	"gorbital.dev/gorbital/mailevents/internal/domain"
)

// A WebhookReader verifies a provider's signed webhook requests and reads
// their events. The module provides one for Resend when
// RESEND_WEBHOOK_SECRET is set, or none.
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
