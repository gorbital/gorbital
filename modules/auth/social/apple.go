package social

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/go-jose/go-jose/v4"
)

const (
	appleAudience = "https://appleid.apple.com"
	// clientSecretTTL is how long each Apple client secret is valid; one is
	// signed per request.
	clientSecretTTL = 5 * time.Minute
)

// Apple server-to-server notification types.
const (
	NotificationEmailDisabled  = "email-disabled"
	NotificationEmailEnabled   = "email-enabled"
	NotificationConsentRevoked = "consent-revoked"
	NotificationAccountDelete  = "account-delete"
)

// AppleConfig configures Sign in with Apple.
type AppleConfig struct {
	// TeamID, KeyID and PrivateKey sign client secrets: the Team ID, and the
	// ID and .p8 key of a Sign in with Apple key.
	TeamID     string
	KeyID      string
	PrivateKey *ecdsa.PrivateKey
	// ServicesID is the web client; empty for native apps only.
	ServicesID string
	// BundleIDs are the iOS apps ID tokens may be issued for.
	BundleIDs  []string
	Endpoints  Endpoints
	HTTPClient *http.Client
	Now        func() time.Time
}

// NewApple returns the Apple provider. It returns [ErrInvalidConfig].
func NewApple(c AppleConfig) (*Provider, error) {
	switch {
	case c.TeamID == "" || c.KeyID == "" || c.PrivateKey == nil:
		return nil, fmt.Errorf("%w: Apple needs the team ID, key ID and private key", ErrInvalidConfig)
	case c.ServicesID == "" && len(c.BundleIDs) == 0:
		return nil, fmt.Errorf("%w: Apple needs a Services ID or bundle IDs", ErrInvalidConfig)
	case c.PrivateKey.Curve != elliptic.P256():
		return nil, fmt.Errorf("%w: the Apple key must be a P-256 key", ErrInvalidConfig)
	}
	p := newProvider(Apple, withDefaults(c.Endpoints, AppleEndpoints()), c.HTTPClient, c.Now)
	p.webClient = c.ServicesID
	if c.ServicesID != "" {
		p.clients = append(p.clients, c.ServicesID)
	}
	p.clients = append(p.clients, c.BundleIDs...)
	p.secret = func(clientID string) (string, error) {
		return appleClientSecret(c.TeamID, c.KeyID, c.PrivateKey, clientID, p.now())
	}
	p.scopes, p.formPost = []string{"name", "email"}, true
	return p, nil
}

// appleClientSecret signs the ES256 JWT Apple takes as client_secret.
func appleClientSecret(teamID, keyID string, key *ecdsa.PrivateKey, clientID string, now time.Time) (string, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: key, KeyID: keyID}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return "", fmt.Errorf("social: Apple client secret: %w", err)
	}
	payload, err := json.Marshal(map[string]any{
		"iss": teamID, "iat": now.Unix(), "exp": now.Add(clientSecretTTL).Unix(), "aud": appleAudience, "sub": clientID,
	})
	if err != nil {
		return "", err
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("social: Apple client secret: %w", err)
	}
	return obj.CompactSerialize()
}

// ParseApplePrivateKey reads a Sign in with Apple .p8 key: a PEM PKCS #8
// P-256 private key. It returns [ErrInvalidConfig].
func ParseApplePrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%w: the Apple key isn't a PEM private key (a .p8 file starts with -----BEGIN PRIVATE KEY-----)", ErrInvalidConfig)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: the Apple key: %v", ErrInvalidConfig, err) //nolint:errorlint // x509 errors aren't API
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("%w: the Apple key must be a P-256 key", ErrInvalidConfig)
	}
	return key, nil
}

// MaxNotificationAge is how old an Apple notification can be when it's
// verified, so an old payload can't be replayed later.
const MaxNotificationAge = time.Hour

// Notification is an Apple server-to-server notification about a person
// who signed in with Apple.
type Notification struct {
	// ID identifies the notification ("jti", or a hash of the payload when
	// Apple sends none). Remember it until IssuedAt plus
	// [MaxNotificationAge] and ignore a notification seen before: a replayed
	// payload carries the same ID.
	ID string
	// Type is one of the Notification constants.
	Type    string
	Subject string
	Email   string
	// PrivateEmail reports an Apple relay address.
	PrivateEmail bool
	// At is when the event happened; IssuedAt when Apple signed the
	// notification.
	At       time.Time
	IssuedAt time.Time
}

// AppleNotification verifies the payload Apple posts to the notification
// endpoint: Apple's signature, issuer, a configured audience, and an issue
// time within [MaxNotificationAge]. It returns [ErrInvalidToken], or
// [ErrInvalidConfig] on a provider other than Apple.
func (p *Provider) AppleNotification(ctx context.Context, payload string) (Notification, error) {
	if p.name != Apple {
		return Notification{}, fmt.Errorf("%w: notifications come from Apple", ErrInvalidConfig)
	}
	body, err := p.keys.VerifySignature(ctx, payload)
	if err != nil {
		return Notification{}, invalid(err)
	}
	var claims struct {
		Issuer   string          `json:"iss"`
		Audience json.RawMessage `json:"aud"`
		IssuedAt int64           `json:"iat"`
		ID       string          `json:"jti"`
		Events   string          `json:"events"`
	}
	if err := json.Unmarshal(body, &claims); err != nil {
		return Notification{}, invalid(err)
	}
	var audiences []string
	if err := json.Unmarshal(claims.Audience, &audiences); err != nil {
		var one string
		if json.Unmarshal(claims.Audience, &one) == nil {
			audiences = []string{one}
		}
	}
	var event struct {
		Type         string   `json:"type"`
		Subject      string   `json:"sub"`
		Email        string   `json:"email"`
		PrivateEmail flexBool `json:"is_private_email"`
		EventTime    int64    `json:"event_time"`
	}
	switch {
	case !slices.Contains(p.ep.Issuers, claims.Issuer):
		return Notification{}, fmt.Errorf("%w: issuer %q", ErrInvalidToken, claims.Issuer)
	case sharedAudience(audiences, p.clients) == "":
		return Notification{}, fmt.Errorf("%w: audience %q isn't a configured client", ErrInvalidToken, audiences)
	case json.Unmarshal([]byte(claims.Events), &event) != nil || event.Type == "" || event.Subject == "":
		return Notification{}, fmt.Errorf("%w: unreadable events", ErrInvalidToken)
	}
	now, issued := p.now(), time.Unix(claims.IssuedAt, 0).UTC()
	if claims.IssuedAt <= 0 || issued.After(now.Add(clockSkew)) || now.Sub(issued) > MaxNotificationAge {
		return Notification{}, fmt.Errorf("%w: notification issued at %s, older than %s", ErrInvalidToken, issued, MaxNotificationAge)
	}
	id := claims.ID
	if id == "" {
		sum := sha256.Sum256([]byte(payload))
		id = hex.EncodeToString(sum[:])
	}
	return Notification{
		ID: id, Type: event.Type, Subject: event.Subject, Email: event.Email, PrivateEmail: bool(event.PrivateEmail),
		At: time.UnixMilli(event.EventTime).UTC(), IssuedAt: issued,
	}, nil
}
