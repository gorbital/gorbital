// Package webhook verifies signed webhook requests: an HMAC-SHA256 signature
// over the raw body, and optionally a delivery ID and a timestamp checked
// against a replay window (ADR-0085). [NewStandard] verifies the Standard
// Webhooks scheme that Svix, Resend and Clerk use; [NewHMAC] covers other
// senders, such as GitHub or Shopify.
//
// A [Verifier] reads only headers and the body, so it works with any router.
// In a gorbital app, guard.Webhook runs it before the route's input is
// parsed:
//
//	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{secret}})
//	gorbital.Post(r, "/v1/webhooks/payments", h.paymentEvent, guard.Public(), guard.Webhook(v))
//
// Verification proves who sent a request and that it wasn't changed; it
// doesn't stop a sender from delivering the same event twice. Handlers act
// idempotently, or remember the delivery ID for at least the tolerance.
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0085).
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A Verifier checks that a webhook request comes from its sender: header is
// the request's headers and body its raw body, exactly as received. It
// returns nil for an authentic request, and an error wrapping
// [ErrInvalidSignature] otherwise. Implementations are safe for concurrent
// use.
type Verifier interface {
	Verify(ctx context.Context, header http.Header, body []byte) error
}

// Errors returned by [HMAC.Verify]. Check them with [errors.Is]; answer
// both with the same 401, so a sender doesn't learn which check failed.
var (
	// ErrInvalidSignature reports a request without the expected headers,
	// or whose signatures don't match the body with any secret.
	ErrInvalidSignature = errors.New("webhook: invalid signature")
	// ErrTimestamp reports a request signed further than the tolerance from
	// now, either way. It wraps [ErrInvalidSignature].
	ErrTimestamp = fmt.Errorf("%w: timestamp outside the tolerance", ErrInvalidSignature)
)

// DefaultTolerance is how far a signed timestamp may be from the receiver's
// clock, either way, unless a configuration sets another.
const DefaultTolerance = 5 * time.Minute

// MinSecretBytes is the shortest secret [NewHMAC] and [NewStandard] accept.
const MinSecretBytes = 16

// MaxIDLength bounds a signed delivery ID, which receivers may store to
// refuse replays.
const MaxIDLength = 255

// Encoding is how a signature is written in its header.
type Encoding int

// Signature encodings.
const (
	// Base64 is standard base64 with padding (Standard Webhooks, Shopify).
	Base64 Encoding = iota
	// Hex is hexadecimal, in either case (GitHub, Slack).
	Hex
)

// HMACConfig configures [NewHMAC].
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

// HMAC verifies HMAC-SHA256 webhook signatures. Create one with [NewHMAC]
// or [NewStandard]. It is safe for concurrent use.
type HMAC struct {
	cfg HMACConfig
}

var _ Verifier = (*HMAC)(nil)

// NewHMAC returns a verifier for cfg. It returns an error when there is no
// secret, a secret is shorter than [MinSecretBytes], SignatureHeader is
// empty, the encoding is unknown or Tolerance is negative. Errors never
// quote a secret.
func NewHMAC(cfg HMACConfig) (*HMAC, error) {
	switch {
	case len(cfg.Secrets) == 0:
		return nil, errors.New("webhook: at least one secret is required")
	case cfg.SignatureHeader == "":
		return nil, errors.New("webhook: the signature header is required")
	case cfg.Encoding != Base64 && cfg.Encoding != Hex:
		return nil, fmt.Errorf("webhook: unknown signature encoding %d", cfg.Encoding)
	case cfg.Tolerance < 0:
		return nil, errors.New("webhook: the tolerance must not be negative")
	}
	secrets := make([][]byte, len(cfg.Secrets))
	for i, s := range cfg.Secrets {
		if len(s) < MinSecretBytes {
			return nil, fmt.Errorf("webhook: secret %d is shorter than %d bytes", i+1, MinSecretBytes)
		}
		secrets[i] = append([]byte(nil), s...)
	}
	cfg.Secrets = secrets
	if cfg.Tolerance == 0 {
		cfg.Tolerance = DefaultTolerance
	}
	if cfg.Signed == nil {
		cfg.Signed = signedContent
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &HMAC{cfg: cfg}, nil
}

// signedContent writes "id.timestamp.body", leaving out absent parts.
func signedContent(w io.Writer, id, timestamp string, body []byte) {
	for _, part := range []string{id, timestamp} {
		if part != "" {
			_, _ = io.WriteString(w, part)
			_, _ = io.WriteString(w, ".")
		}
	}
	_, _ = w.Write(body)
}

// Verify checks a request's headers and raw body. It returns nil when the
// configured headers are present and well formed, the timestamp (if any) is
// within the tolerance, and at least one signature entry is the HMAC-SHA256
// of the signed content with one of the secrets. It returns [ErrTimestamp]
// for a timestamp outside the tolerance and [ErrInvalidSignature] for
// anything else. Every entry is compared with every secret in constant
// time, so timing doesn't reveal which one matched.
func (v *HMAC) Verify(_ context.Context, header http.Header, body []byte) error {
	c := &v.cfg
	var id, timestamp string
	if c.IDHeader != "" {
		id = header.Get(c.IDHeader)
		if id == "" || len(id) > MaxIDLength || strings.ContainsAny(id, ". \t\r\n") {
			return ErrInvalidSignature
		}
	}
	signatures := header.Get(c.SignatureHeader)
	if signatures == "" {
		return ErrInvalidSignature
	}
	if c.TimestampHeader != "" {
		timestamp = header.Get(c.TimestampHeader)
		seconds, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || seconds <= 0 {
			return ErrInvalidSignature
		}
		signedAt, now := time.Unix(seconds, 0), c.Now()
		if signedAt.Before(now.Add(-c.Tolerance)) || signedAt.After(now.Add(c.Tolerance)) {
			return ErrTimestamp
		}
	}

	expected := make([][]byte, len(c.Secrets))
	for i, secret := range c.Secrets {
		mac := hmac.New(sha256.New, secret)
		c.Signed(mac, id, timestamp, body)
		expected[i] = mac.Sum(nil)
	}
	valid := false
	for _, entry := range strings.Fields(signatures) {
		encoded, ok := strings.CutPrefix(entry, c.SignaturePrefix)
		if !ok {
			continue
		}
		sig, err := c.decode(encoded)
		if err != nil {
			continue
		}
		for _, e := range expected {
			if hmac.Equal(sig, e) {
				valid = true
			}
		}
	}
	if !valid {
		return ErrInvalidSignature
	}
	return nil
}

func (c *HMACConfig) decode(s string) ([]byte, error) {
	if c.Encoding == Hex {
		return hex.DecodeString(s)
	}
	return base64.StdEncoding.DecodeString(s)
}

// StandardConfig configures [NewStandard].
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

// StandardSecretPrefix starts Standard Webhooks signing secrets.
const StandardSecretPrefix = "whsec_"

// NewStandard returns a verifier for the Standard Webhooks scheme
// (https://www.standardwebhooks.com): headers "<prefix>id",
// "<prefix>timestamp" (Unix seconds) and "<prefix>signature" (space-separated
// "v1,<base64>" entries), signed with HMAC-SHA256 over
// "<id>.<timestamp>.<body>". Retries of one event keep the ID and get a new
// timestamp. It returns an error when there is no secret or a secret isn't
// base64 of at least [MinSecretBytes] bytes, without quoting it.
func NewStandard(cfg StandardConfig) (*HMAC, error) {
	if len(cfg.Secrets) == 0 {
		return nil, errors.New("webhook: at least one secret is required")
	}
	keys := make([][]byte, len(cfg.Secrets))
	for i, s := range cfg.Secrets {
		key, err := DecodeStandardSecret(s)
		if err != nil {
			if len(cfg.Secrets) > 1 {
				return nil, fmt.Errorf("secret %d: %w", i+1, err)
			}
			return nil, err
		}
		keys[i] = key
	}
	prefix := cfg.HeaderPrefix
	if prefix == "" {
		prefix = "webhook-"
	}
	return NewHMAC(HMACConfig{
		Secrets:         keys,
		SignatureHeader: prefix + "signature",
		SignaturePrefix: "v1,",
		Encoding:        Base64,
		IDHeader:        prefix + "id",
		TimestampHeader: prefix + "timestamp",
		Tolerance:       cfg.Tolerance,
		Now:             cfg.Now,
	})
}

// DecodeStandardSecret decodes a Standard Webhooks signing secret, with or
// without its "whsec_" prefix, so apps can refuse a mistyped secret when
// they start. It returns an error, never quoting the secret, when the
// secret is empty, isn't base64 or is shorter than [MinSecretBytes] bytes.
func DecodeStandardSecret(secret string) ([]byte, error) {
	encoded := strings.TrimPrefix(secret, StandardSecretPrefix)
	if encoded == "" {
		return nil, errors.New("webhook: the signing secret is empty")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) < MinSecretBytes {
		return nil, fmt.Errorf("webhook: the signing secret must be %s followed by base64 of at least %d bytes", StandardSecretPrefix, MinSecretBytes)
	}
	return key, nil
}
