# modules/mail/resend

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/mail/resend"
```

Package resend sends email with Resend ([https://resend.com](https://resend.com)) through its HTTP API (ADR-0025, ADR-0037).

```go
sender, err := resend.New(cfg.ResendAPIKey)
```

The API key is a secret from the environment (RESEND\_API\_KEY). Sender names and addresses are chosen per message; apps fill them from runtime settings with mail.WithDefaults.

Refusals that retrying can't fix (invalid messages, unverified sender domains) wrap [mail.ErrRejected](mail.md#ErrRejected). Rate limits, outages and an invalid API key (fixable by an operator) are temporary. Each message's idempotency key is sent as Resend's Idempotency-Key, so a retried job never sends twice.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultBaseURL`](#DefaultBaseURL), [`DefaultTimeout`](#DefaultTimeout), [`HeaderWebhookID`](#HeaderWebhookID), [`HeaderWebhookTimestamp`](#HeaderWebhookTimestamp), [`HeaderWebhookSignature`](#HeaderWebhookSignature), [`EventEmailBounced`](#EventEmailBounced), [`EventEmailComplained`](#EventEmailComplained), [`EventEmailDelivered`](#EventEmailDelivered), [`BouncePermanent`](#BouncePermanent), [`BounceTransient`](#BounceTransient), [`BounceUndetermined`](#BounceUndetermined), [`WebhookTolerance`](#WebhookTolerance)
- Variables: [`ErrInvalidWebhook`](#ErrInvalidWebhook), [`ErrWebhookTimestamp`](#ErrWebhookTimestamp)
- Functions: [`CheckWebhookSecret`](#CheckWebhookSecret), [`VerifyWebhook`](#VerifyWebhook)
- Types:
  - [`Bounce`](#Bounce): [`Bounce.Permanent`](#Bounce.Permanent)
  - [`Option`](#Option): [`WithBaseURL`](#WithBaseURL), [`WithHTTPClient`](#WithHTTPClient), [`WithTimeout`](#WithTimeout)
  - [`Sender`](#Sender): [`New`](#New), [`Sender.Send`](#Sender.Send)
  - [`WebhookEvent`](#WebhookEvent): [`ParseWebhookEvent`](#ParseWebhookEvent)

## Constants

<a id="DefaultBaseURL"></a>
<a id="DefaultTimeout"></a>

```go
const (
	DefaultBaseURL = "https://api.resend.com"
	DefaultTimeout = 30 * time.Second
)
```

Defaults.

*Since `v0.1.0`*

<a id="HeaderWebhookID"></a>
<a id="HeaderWebhookTimestamp"></a>
<a id="HeaderWebhookSignature"></a>

```go
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
```

Webhook headers. Resend signs its webhooks with Svix ([https://docs.svix.com/receiving/verifying-payloads/how-manual](https://docs.svix.com/receiving/verifying-payloads/how-manual)).

*Since `v0.1.0`*

<a id="EventEmailBounced"></a>
<a id="EventEmailComplained"></a>
<a id="EventEmailDelivered"></a>

```go
const (
	EventEmailBounced    = "email.bounced"
	EventEmailComplained = "email.complained"
	EventEmailDelivered  = "email.delivered"
)
```

Webhook event types gorbital acts on. Resend sends others too (sent, delivery\_delayed, opened, clicked…); receivers accept and ignore them.

*Since `v0.1.0`*

<a id="BouncePermanent"></a>
<a id="BounceTransient"></a>
<a id="BounceUndetermined"></a>

```go
const (
	// BouncePermanent is a hard bounce: the address doesn't accept email.
	BouncePermanent = "Permanent"
	// BounceTransient is a soft bounce, such as a full mailbox.
	BounceTransient = "Transient"
	// BounceUndetermined is a bounce Resend couldn't classify.
	BounceUndetermined = "Undetermined"
)
```

Bounce types in [Bounce.Type](#Bounce.Type) ([https://resend.com/docs/dashboard/emails/email-bounces](https://resend.com/docs/dashboard/emails/email-bounces)).

*Since `v0.1.0`*

<a id="WebhookTolerance"></a>

```go
const WebhookTolerance = 5 * time.Minute
```

WebhookTolerance is how far a webhook's timestamp may be from the receiver's clock, either way. Older requests are refused, so a captured request can't be replayed later.

*Since `v0.1.0`*

## Variables

<a id="ErrInvalidWebhook"></a>
<a id="ErrWebhookTimestamp"></a>

```go
var (
	// ErrInvalidWebhook reports a request without Svix headers, or whose
	// signatures don't match the body with the secret.
	ErrInvalidWebhook = errors.New("resend: invalid webhook signature")
	// ErrWebhookTimestamp reports a request signed more than
	// [WebhookTolerance] away from now.
	ErrWebhookTimestamp = fmt.Errorf("%w: timestamp outside the tolerance", ErrInvalidWebhook)
)
```

Errors returned by [VerifyWebhook](#VerifyWebhook). Check them with [errors.Is](https://pkg.go.dev/errors#Is); answer both with 401 so the sender doesn't learn which check failed.

*Since `v0.1.0`*

## Functions

<a id="CheckWebhookSecret"></a>

### func CheckWebhookSecret

```go
func CheckWebhookSecret(secret config.Secret) error
```

CheckWebhookSecret reports whether secret is a well-formed signing secret, so apps can refuse a mistyped RESEND\_WEBHOOK\_SECRET when they start.

*Since `v0.1.0`*

<a id="VerifyWebhook"></a>

### func VerifyWebhook

```go
func VerifyWebhook(secret config.Secret, header http.Header, body []byte, now time.Time) error
```

VerifyWebhook checks a webhook request from Resend: its svix-id, svix-timestamp and svix-signature headers, and body, the raw request body exactly as received. It returns nil when the timestamp is within [WebhookTolerance](#WebhookTolerance) of now and at least one v1 signature is the base64 HMAC-SHA256, keyed with secret, of "\<svix-id>.\<svix-timestamp>.\<body>". Signatures are compared in constant time; other versions are ignored.

A valid request can be delivered more than once: receivers remember the svix-id for at least the tolerance to refuse replays, and act idempotently on retries, which Svix sends with the same ID and a new timestamp.

*Since `v0.1.0`*

## Types

<a id="Bounce"></a>
<a id="Bounce.Type"></a>
<a id="Bounce.SubType"></a>
<a id="Bounce.Message"></a>

### type Bounce

```go
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
```

A Bounce describes why an email bounced.

*Since `v0.1.0`*

<a id="Bounce.Permanent"></a>

#### func (Bounce) Permanent

```go
func (b Bounce) Permanent() bool
```

Permanent reports whether the bounce is a hard bounce, after which the address should not receive email again.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [New](#New).

*Since `v0.1.0`*

<a id="WithBaseURL"></a>

#### func WithBaseURL

```go
func WithBaseURL(u string) Option
```

WithBaseURL sets the API base URL, for tests. Default: [DefaultBaseURL](#DefaultBaseURL).

*Since `v0.1.0`*

<a id="WithHTTPClient"></a>

#### func WithHTTPClient

```go
func WithHTTPClient(c *http.Client) Option
```

WithHTTPClient sets the HTTP client, for example one with a proxy. Its own timeout applies instead of [WithTimeout](#WithTimeout).

*Since `v0.1.0`*

<a id="WithTimeout"></a>

#### func WithTimeout

```go
func WithTimeout(d time.Duration) Option
```

WithTimeout bounds each API request. Default: [DefaultTimeout](#DefaultTimeout).

*Since `v0.1.0`*

<a id="Sender"></a>

### type Sender

```go
type Sender struct {
	// contains filtered or unexported fields
}
```

Sender delivers email through the Resend API. It is safe for concurrent use.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(apiKey config.Secret, opts ...Option) (*Sender, error)
```

New returns a sender using apiKey, a Resend API key such as "re\_123…".

*Since `v0.1.0`*

<a id="Sender.Send"></a>

#### func (*Sender) Send

```go
func (s *Sender) Send(ctx context.Context, m mail.Message) error
```

Send delivers m.

*Since `v0.1.0`*

<a id="WebhookEvent"></a>
<a id="WebhookEvent.Type"></a>
<a id="WebhookEvent.CreatedAt"></a>
<a id="WebhookEvent.EmailID"></a>
<a id="WebhookEvent.To"></a>
<a id="WebhookEvent.Bounce"></a>

### type WebhookEvent

```go
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
```

A WebhookEvent is one event Resend posts to a webhook.

*Since `v0.1.0`*

<a id="ParseWebhookEvent"></a>

#### func ParseWebhookEvent

```go
func ParseWebhookEvent(body []byte) (WebhookEvent, error)
```

ParseWebhookEvent reads a webhook body. Call it only after [VerifyWebhook](#VerifyWebhook) accepted the request. A body that isn't an event returns an error; unknown fields and event types are accepted.

*Since `v0.1.0`*
