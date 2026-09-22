# webhook

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/webhook"
```

Package webhook verifies signed webhook requests: an HMAC-SHA256 signature over the raw body, and optionally a delivery ID and a timestamp checked against a replay window (ADR-0085). [NewStandard](#NewStandard) verifies the Standard Webhooks scheme that Svix, Resend and Clerk use; [NewHMAC](#NewHMAC) covers other senders, such as GitHub or Shopify.

A [Verifier](#Verifier) reads only headers and the body, so it works with any router. In a gorbital app, guard.Webhook runs it before the route's input is parsed:

```go
v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{secret}})
gorbital.Post(r, "/v1/webhooks/payments", h.paymentEvent, guard.Public(), guard.Webhook(v))
```

Verification proves who sent a request and that it wasn't changed; it doesn't stop a sender from delivering the same event twice. Handlers act idempotently, or remember the delivery ID for at least the tolerance.

Stability: experimental until v0.2.0 (ADR-0015, ADR-0085).

## When to use

When your app receives webhooks: verify each request's signature before acting on it. In a gorbital app, pass the verifier to `guard.Webhook` on the route, which checks it before the body is parsed; elsewhere call [HMAC.Verify](#HMAC.Verify) with the raw body.

- [Security layers](../guides/security-layers.md#signed-webhooks): sender settings, secret rotation, replays and a custom verifier.
- [Guards and middleware](../guides/guards-and-middleware.md#webhooks).

## Contents

- Constants: [`DefaultTolerance`](#DefaultTolerance), [`MaxIDLength`](#MaxIDLength), [`MinSecretBytes`](#MinSecretBytes), [`StandardSecretPrefix`](#StandardSecretPrefix)
- Variables: [`ErrInvalidSignature`](#ErrInvalidSignature), [`ErrTimestamp`](#ErrTimestamp)
- Functions: [`DecodeStandardSecret`](#DecodeStandardSecret)
- Types:
  - [`Encoding`](#Encoding): [`Base64`](#Base64), [`Hex`](#Hex)
  - [`HMAC`](#HMAC): [`NewHMAC`](#NewHMAC), [`NewStandard`](#NewStandard), [`HMAC.Verify`](#HMAC.Verify)
  - [`HMACConfig`](#HMACConfig)
  - [`StandardConfig`](#StandardConfig)
  - [`Verifier`](#Verifier)

## Constants

<a id="DefaultTolerance"></a>

```go
const DefaultTolerance = 5 * time.Minute
```

DefaultTolerance is how far a signed timestamp may be from the receiver's clock, either way, unless a configuration sets another.

*Since `v0.2.0`*

<a id="MaxIDLength"></a>

```go
const MaxIDLength = 255
```

MaxIDLength bounds a signed delivery ID, which receivers may store to refuse replays.

*Since `v0.2.0`*

<a id="MinSecretBytes"></a>

```go
const MinSecretBytes = 16
```

MinSecretBytes is the shortest secret [NewHMAC](#NewHMAC) and [NewStandard](#NewStandard) accept.

*Since `v0.2.0`*

<a id="StandardSecretPrefix"></a>

```go
const StandardSecretPrefix = "whsec_"
```

StandardSecretPrefix starts Standard Webhooks signing secrets.

*Since `v0.2.0`*

## Variables

<a id="ErrInvalidSignature"></a>
<a id="ErrTimestamp"></a>

```go
var (
	// ErrInvalidSignature reports a request without the expected headers,
	// or whose signatures don't match the body with any secret.
	ErrInvalidSignature = errors.New("webhook: invalid signature")
	// ErrTimestamp reports a request signed further than the tolerance from
	// now, either way. It wraps [ErrInvalidSignature].
	ErrTimestamp = fmt.Errorf("%w: timestamp outside the tolerance", ErrInvalidSignature)
)
```

Errors returned by [HMAC.Verify](#HMAC.Verify). Check them with [errors.Is](https://pkg.go.dev/errors#Is); answer both with the same 401, so a sender doesn't learn which check failed.

*Since `v0.2.0`*

## Functions

<a id="DecodeStandardSecret"></a>

### func DecodeStandardSecret

```go
func DecodeStandardSecret(secret string) ([]byte, error)
```

DecodeStandardSecret decodes a Standard Webhooks signing secret, with or without its "whsec\_" prefix, so apps can refuse a mistyped secret when they start. It returns an error, never quoting the secret, when the secret is empty, isn't base64 or is shorter than [MinSecretBytes](#MinSecretBytes) bytes.

*Since `v0.2.0`*

**Example**

```go
_, err := webhook.DecodeStandardSecret("whsec_c2hvcnQ=")
fmt.Println(err)
```

Output:

```text
webhook: the signing secret must be whsec_ followed by base64 of at least 16 bytes
```

## Types

<a id="Encoding"></a>

### type Encoding

```go
type Encoding int
```

Encoding is how a signature is written in its header.

*Since `v0.2.0`*

**Example**

```go
// Shopify: base64 (the default) over the body, no prefix.
shopify, err := webhook.NewHMAC(webhook.HMACConfig{
	Secrets:         [][]byte{[]byte("shpss_0123456789abcdef0123456789")}, // gitleaks:allow (example)
	SignatureHeader: "X-Shopify-Hmac-Sha256",
	Encoding:        webhook.Base64,
})
if err != nil {
	panic(err)
}
mac := hmac.New(sha256.New, []byte("shpss_0123456789abcdef0123456789"))
mac.Write([]byte(`{"id":1}`))
header := http.Header{"X-Shopify-Hmac-Sha256": {base64.StdEncoding.EncodeToString(mac.Sum(nil))}}
fmt.Println(shopify.Verify(context.Background(), header, []byte(`{"id":1}`)))
```

Output:

```text
<nil>
```

<a id="Base64"></a>
<a id="Hex"></a>

```go
const (
	// Base64 is standard base64 with padding (Standard Webhooks, Shopify).
	Base64 Encoding = iota
	// Hex is hexadecimal, in either case (GitHub, Slack).
	Hex
)
```

Signature encodings.

*Since `v0.2.0`*

<a id="HMAC"></a>

### type HMAC

```go
type HMAC struct {
	// contains filtered or unexported fields
}
```

HMAC verifies HMAC-SHA256 webhook signatures. Create one with [NewHMAC](#NewHMAC) or [NewStandard](#NewStandard). It is safe for concurrent use.

*Since `v0.2.0`*

**Example**

```go
// One *HMAC serves every request; build it once at start.
v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{exampleSecret}})
if err != nil {
	panic(err)
}
body := `{"type":"invoice.paid"}`
for _, id := range []string{"msg_1", "msg_2"} {
	fmt.Println(id, v.Verify(context.Background(), signed(id, time.Now(), body), []byte(body)))
}
```

Output:

```text
msg_1 <nil>
msg_2 <nil>
```

<a id="NewHMAC"></a>

#### func NewHMAC

```go
func NewHMAC(cfg HMACConfig) (*HMAC, error)
```

NewHMAC returns a verifier for cfg. It returns an error when there is no secret, a secret is shorter than [MinSecretBytes](#MinSecretBytes), SignatureHeader is empty, the encoding is unknown or Tolerance is negative. Errors never quote a secret.

*Since `v0.2.0`*

**Example**

```go
// GitHub: "X-Hub-Signature-256: sha256=<hex>" over the body alone.
v, err := webhook.NewHMAC(webhook.HMACConfig{
	Secrets:         [][]byte{[]byte("It's a Secret to Everybody")}, // gitleaks:allow (GitHub's published test vector)
	SignatureHeader: "X-Hub-Signature-256",
	SignaturePrefix: "sha256=",
	Encoding:        webhook.Hex,
})
if err != nil {
	panic(err)
}
header := http.Header{}
header.Set("X-Hub-Signature-256", "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17")
fmt.Println(v.Verify(context.Background(), header, []byte("Hello, World!")))
```

Output:

```text
<nil>
```

<a id="NewStandard"></a>

#### func NewStandard

```go
func NewStandard(cfg StandardConfig) (*HMAC, error)
```

NewStandard returns a verifier for the Standard Webhooks scheme ([https://www.standardwebhooks.com](https://www.standardwebhooks.com)): headers "\<prefix>id", "\<prefix>timestamp" (Unix seconds) and "\<prefix>signature" (space-separated "v1,\<base64>" entries), signed with HMAC-SHA256 over "\<id>.\<timestamp>.\<body>". Retries of one event keep the ID and get a new timestamp. It returns an error when there is no secret or a secret isn't base64 of at least [MinSecretBytes](#MinSecretBytes) bytes, without quoting it.

*Since `v0.2.0`*

**Example**

```go
v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{exampleSecret}})
if err != nil {
	panic(err)
}
body := `{"type":"payment.succeeded"}`
header := signed("msg_1", time.Now(), body)

fmt.Println(v.Verify(context.Background(), header, []byte(body)))
err = v.Verify(context.Background(), header, []byte(`{"type":"payment.refunded"}`))
fmt.Println(errors.Is(err, webhook.ErrInvalidSignature))
```

Output:

```text
<nil>
true
```

**Example (svix)**

```go
// Resend and Clerk send Svix's header names.
v, err := webhook.NewStandard(webhook.StandardConfig{
	Secrets:      []string{"whsec_plJ3nmyCDGBKInavdOK15jsl"}, // gitleaks:allow (Svix's published test vector)
	HeaderPrefix: "svix-",
	Now:          func() time.Time { return time.Unix(1731705121, 0) },
})
if err != nil {
	panic(err)
}
header := http.Header{}
header.Set("svix-id", "msg_loFOjxBNrRLzqYUf")
header.Set("svix-timestamp", "1731705121")
header.Set("svix-signature", "v1,rAvfW3dJ/X/qxhsaXPOyyCGmRKsaKWcsNccKXlIktD0=")
fmt.Println(v.Verify(context.Background(), header, []byte(`{"event_type":"ping","data":{"success":true}}`)))
```

Output:

```text
<nil>
```

<a id="HMAC.Verify"></a>

#### func (*HMAC) Verify

```go
func (v *HMAC) Verify(_ context.Context, header http.Header, body []byte) error
```

Verify checks a request's headers and raw body. It returns nil when the configured headers are present and well formed, the timestamp (if any) is within the tolerance, and at least one signature entry is the HMAC-SHA256 of the signed content with one of the secrets. It returns [ErrTimestamp](#ErrTimestamp) for a timestamp outside the tolerance and [ErrInvalidSignature](#ErrInvalidSignature) for anything else. Every entry is compared with every secret in constant time, so timing doesn't reveal which one matched.

*Since `v0.2.0`*

**Example**

```go
v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{exampleSecret}})
if err != nil {
	panic(err)
}
mux := http.NewServeMux()
mux.HandleFunc("POST /webhooks/payments", func(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	if err := v.Verify(r.Context(), r.Header, body); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	// Act on the event, idempotently: retries keep the webhook-id.
	w.WriteHeader(http.StatusNoContent)
})
```

<a id="HMACConfig"></a>
<a id="HMACConfig.Secrets"></a>
<a id="HMACConfig.SignatureHeader"></a>
<a id="HMACConfig.SignaturePrefix"></a>
<a id="HMACConfig.Encoding"></a>
<a id="HMACConfig.IDHeader"></a>
<a id="HMACConfig.TimestampHeader"></a>
<a id="HMACConfig.Tolerance"></a>
<a id="HMACConfig.Signed"></a>
<a id="HMACConfig.Now"></a>

### type HMACConfig

```go
type HMACConfig struct {
	// Secrets are the signing keys. A signature made with any of them is
	// accepted, so a sender's secret can be rotated: add the new one, deploy,
	// switch the sender, then remove the old one. Each is at least
	// [MinSecretBytes] long.
	Secrets [][]byte
	// SignatureHeader holds the signature: one entry, or several separated by
	// spaces. Required.
	SignatureHeader string
	// SignaturePrefix starts every accepted entry, such as "v1," or
	// "sha256="; entries with another prefix are ignored.
	SignaturePrefix string
	// Encoding is how the signature after the prefix is encoded.
	Encoding Encoding
	// IDHeader, when set, holds a delivery ID that is part of the signed
	// content. It must be present, at most [MaxIDLength] bytes, and contain
	// no dots or whitespace.
	IDHeader string
	// TimestampHeader, when set, holds the Unix time in seconds at which the
	// request was signed, which is part of the signed content. Requests
	// signed further than Tolerance from now are refused, so a captured
	// request can't be replayed later. Without it there is no replay window:
	// use it whenever the sender signs a timestamp.
	TimestampHeader string
	// Tolerance is the replay window either side of now. Default:
	// [DefaultTolerance].
	Tolerance time.Duration
	// Signed writes the signed content for a request to w. Default: the
	// ID, the timestamp and the body, each present part followed by a dot
	// except the body ("id.timestamp.body", "timestamp.body" or "body").
	Signed func(w io.Writer, id, timestamp string, body []byte)
	// Now returns the current time. Default: [time.Now].
	Now func() time.Time
}
```

HMACConfig configures [NewHMAC](#NewHMAC).

*Since `v0.2.0`*

**Example**

```go
// Slack signs "v0:<timestamp>:<body>" and sends "v0=<hex>".
v, err := webhook.NewHMAC(webhook.HMACConfig{
	Secrets:         [][]byte{[]byte("8f742231b10e8888abcd99yyyzzz85a5")}, // gitleaks:allow (example)
	SignatureHeader: "X-Slack-Signature",
	SignaturePrefix: "v0=",
	Encoding:        webhook.Hex,
	TimestampHeader: "X-Slack-Request-Timestamp",
	Signed: func(w io.Writer, _, timestamp string, body []byte) {
		_, _ = io.WriteString(w, "v0:"+timestamp+":")
		_, _ = w.Write(body)
	},
})
if err != nil {
	panic(err)
}
header := http.Header{}
header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().Unix(), 10))
header.Set("X-Slack-Signature", "v0=0000")
fmt.Println(errors.Is(v.Verify(context.Background(), header, []byte("token=x")), webhook.ErrInvalidSignature))
```

Output:

```text
true
```

<a id="StandardConfig"></a>
<a id="StandardConfig.Secrets"></a>
<a id="StandardConfig.HeaderPrefix"></a>
<a id="StandardConfig.Tolerance"></a>
<a id="StandardConfig.Now"></a>

### type StandardConfig

```go
type StandardConfig struct {
	// Secrets are signing secrets as the sender shows them: "whsec_"
	// followed by base64, or the base64 alone. Several are accepted during a
	// rotation.
	Secrets []string
	// HeaderPrefix starts the three header names. Default: "webhook-"
	// (webhook-id, webhook-timestamp, webhook-signature). Use "svix-" for
	// senders that use Svix's names, such as Resend and Clerk.
	HeaderPrefix string
	// Tolerance is the replay window either side of now. Default:
	// [DefaultTolerance].
	Tolerance time.Duration
	// Now returns the current time. Default: [time.Now].
	Now func() time.Time
}
```

StandardConfig configures [NewStandard](#NewStandard).

*Since `v0.2.0`*

**Example**

```go
// During a rotation, accept the old and the new secret.
v, err := webhook.NewStandard(webhook.StandardConfig{
	Secrets: []string{
		"whsec_ZmVkY2JhOTg3NjU0MzIxMGZlZGNiYTk4NzY1NDMyMTA=", // gitleaks:allow (example: the new secret)
		exampleSecret, // the old secret
	},
	Tolerance: 3 * time.Minute,
})
if err != nil {
	panic(err)
}
body := `{}`
fmt.Println(v.Verify(context.Background(), signed("msg_1", time.Now(), body), []byte(body)))
err = v.Verify(context.Background(), signed("msg_2", time.Now().Add(-4*time.Minute), body), []byte(body))
fmt.Println(errors.Is(err, webhook.ErrTimestamp))
```

Output:

```text
<nil>
true
```

<a id="Verifier"></a>
<a id="Verifier.Verify"></a>

### type Verifier

```go
type Verifier interface {
	Verify(ctx context.Context, header http.Header, body []byte) error
}
```

A Verifier checks that a webhook request comes from its sender: header is the request's headers and body its raw body, exactly as received. It returns nil for an authentic request, and an error wrapping [ErrInvalidSignature](#ErrInvalidSignature) otherwise. Implementations are safe for concurrent use.

*Since `v0.2.0`*

**Example**

```go
// Any type with a Verify method can guard a route, such as a verifier
// for a sender that signs with a public key.
var v webhook.Verifier = verifierFunc(func(_ context.Context, header http.Header, _ []byte) error {
	if header.Get("X-Test-Signature") != "ok" {
		return webhook.ErrInvalidSignature
	}
	return nil
})
fmt.Println(v.Verify(context.Background(), http.Header{"X-Test-Signature": {"ok"}}, nil))
```

Output:

```text
<nil>
```
