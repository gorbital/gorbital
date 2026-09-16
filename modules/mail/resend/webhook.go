package resend

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/config"
)

// Webhook headers. Resend signs its webhooks with Svix
// (https://docs.svix.com/receiving/verifying-payloads/how-manual).
const (
	// HeaderWebhookID is the delivery's message ID, the same for every retry
	// of one event.
	HeaderWebhookID = "svix-id"
	// HeaderWebhookTimestamp is when this attempt was signed, in Unix seconds.
	HeaderWebhookTimestamp = "svix-timestamp"
	// HeaderWebhookSignature holds space-separated signatures such as
	// "v1,<base64>": more than one while a secret is being rotated.
	HeaderWebhookSignature = "svix-signature"
)

// WebhookTolerance is how far a webhook's timestamp may be from the
// receiver's clock, either way. Older requests are refused, so a captured
// request can't be replayed later.
const WebhookTolerance = 5 * time.Minute

// Webhook secret format: "whsec_" followed by base64.
const webhookSecretPrefix = "whsec_"

// maxWebhookIDLength bounds the delivery ID a receiver stores to refuse
// replays.
const maxWebhookIDLength = 255

// Errors returned by [VerifyWebhook]. Check them with [errors.Is]; answer
// both with 401 so the sender doesn't learn which check failed.
var (
	// ErrInvalidWebhook reports a request without Svix headers, or whose
	// signatures don't match the body with the secret.
	ErrInvalidWebhook = errors.New("resend: invalid webhook signature")
	// ErrWebhookTimestamp reports a request signed more than
	// [WebhookTolerance] away from now.
	ErrWebhookTimestamp = fmt.Errorf("%w: timestamp outside the tolerance", ErrInvalidWebhook)
)

// webhookKey decodes a signing secret from the Resend dashboard, with or
// without its "whsec_" prefix.
func webhookKey(secret config.Secret) ([]byte, error) {
	encoded := strings.TrimPrefix(secret.Reveal(), webhookSecretPrefix)
	if encoded == "" {
		return nil, errors.New("resend: the webhook signing secret is empty")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) < 16 {
		// Never quote the secret.
		return nil, errors.New("resend: the webhook signing secret must be whsec_ followed by base64, as shown in Resend's webhook settings")
	}
	return key, nil
}

// CheckWebhookSecret reports whether secret is a well-formed signing secret,
// so apps can refuse a mistyped RESEND_WEBHOOK_SECRET when they start.
func CheckWebhookSecret(secret config.Secret) error {
	_, err := webhookKey(secret)
	return err
}

// VerifyWebhook checks a webhook request from Resend: its svix-id,
// svix-timestamp and svix-signature headers, and body, the raw request body
// exactly as received. It returns nil when the timestamp is within
// [WebhookTolerance] of now and at least one v1 signature is the base64
// HMAC-SHA256, keyed with secret, of "<svix-id>.<svix-timestamp>.<body>".
// Signatures are compared in constant time; other versions are ignored.
//
// A valid request can be delivered more than once: receivers remember the
// svix-id for at least the tolerance to refuse replays, and act
// idempotently on retries, which Svix sends with the same ID and a new
// timestamp.
func VerifyWebhook(secret config.Secret, header http.Header, body []byte, now time.Time) error {
	key, err := webhookKey(secret)
	if err != nil {
		return err
	}
	id, timestamp, signatures := header.Get(HeaderWebhookID), header.Get(HeaderWebhookTimestamp), header.Get(HeaderWebhookSignature)
	if id == "" || len(id) > maxWebhookIDLength || strings.ContainsAny(id, ". \t\r\n") || signatures == "" {
		return ErrInvalidWebhook
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || seconds <= 0 {
		return ErrInvalidWebhook
	}
	signedAt := time.Unix(seconds, 0)
	if signedAt.Before(now.Add(-WebhookTolerance)) || signedAt.After(now.Add(WebhookTolerance)) {
		return ErrWebhookTimestamp
	}

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	expected := mac.Sum(nil)
	valid := false
	for _, entry := range strings.Fields(signatures) {
		version, encoded, ok := strings.Cut(entry, ",")
		if !ok || version != "v1" {
			continue
		}
		sig, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			continue
		}
		// Check every entry, so timing doesn't tell which one matched.
		if hmac.Equal(sig, expected) {
			valid = true
		}
	}
	if !valid {
		return ErrInvalidWebhook
	}
	return nil
}

// Webhook event types gorbital acts on. Resend sends others too (sent,
// delivery_delayed, opened, clicked…); receivers accept and ignore them.
const (
	EventEmailBounced    = "email.bounced"
	EventEmailComplained = "email.complained"
	EventEmailDelivered  = "email.delivered"
)

// Bounce types in [Bounce.Type]
// (https://resend.com/docs/dashboard/emails/email-bounces).
const (
	// BouncePermanent is a hard bounce: the address doesn't accept email.
	BouncePermanent = "Permanent"
	// BounceTransient is a soft bounce, such as a full mailbox.
	BounceTransient = "Transient"
	// BounceUndetermined is a bounce Resend couldn't classify.
	BounceUndetermined = "Undetermined"
)

// A WebhookEvent is one event Resend posts to a webhook.
type WebhookEvent struct {
	// Type is the event type, such as [EventEmailBounced].
	Type      string
	CreatedAt time.Time
	// EmailID is the ID Resend returned when the email was sent.
	EmailID string
	// To are the email's recipients.
	To []string
	// Bounce is set for [EventEmailBounced].
	Bounce *Bounce
}

// A Bounce describes why an email bounced.
type Bounce struct {
	// Type is [BouncePermanent], [BounceTransient] or [BounceUndetermined].
	Type string
	// SubType refines Type, such as General, NoEmail, Suppressed or
	// MailboxFull.
	SubType string
	// Message is the receiving server's explanation. It can quote the
	// recipient: redact it before logging or storing it.
	Message string
}

// Permanent reports whether the bounce is a hard bounce, after which the
// address should not receive email again.
func (b Bounce) Permanent() bool { return b.Type == BouncePermanent }

type webhookPayload struct {
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Data      struct {
		EmailID string          `json:"email_id"`
		To      json.RawMessage `json:"to"`
		Bounce  *struct {
			Type    string `json:"type"`
			SubType string `json:"subType"`
			Message string `json:"message"`
		} `json:"bounce"`
	} `json:"data"`
}

// ParseWebhookEvent reads a webhook body. Call it only after
// [VerifyWebhook] accepted the request. A body that isn't an event returns an
// error; unknown fields and event types are accepted.
func ParseWebhookEvent(body []byte) (WebhookEvent, error) {
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return WebhookEvent{}, fmt.Errorf("resend: webhook body is not an event: %w", err)
	}
	if p.Type == "" {
		return WebhookEvent{}, errors.New("resend: webhook event has no type")
	}
	e := WebhookEvent{Type: p.Type, CreatedAt: p.CreatedAt, EmailID: p.Data.EmailID}
	// "to" is a list; accept a single address too.
	if len(p.Data.To) > 0 && string(p.Data.To) != "null" {
		if err := json.Unmarshal(p.Data.To, &e.To); err != nil {
			var one string
			if json.Unmarshal(p.Data.To, &one) != nil {
				return WebhookEvent{}, errors.New("resend: webhook event recipients are not a list of addresses")
			}
			e.To = []string{one}
		}
	}
	if b := p.Data.Bounce; b != nil {
		e.Bounce = &Bounce{Type: b.Type, SubType: b.SubType, Message: b.Message}
	}
	if (e.Type == EventEmailBounced || e.Type == EventEmailComplained) && len(e.To) == 0 {
		return WebhookEvent{}, fmt.Errorf("resend: %s event has no recipients", e.Type)
	}
	if e.Type == EventEmailBounced && e.Bounce == nil {
		return WebhookEvent{}, errors.New("resend: email.bounced event has no bounce")
	}
	return e, nil
}
