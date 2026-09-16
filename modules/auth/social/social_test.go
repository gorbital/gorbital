package social_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"gorbital.dev/modules/auth/social"
	"gorbital.dev/modules/auth/social/socialtest"
)

const redirect = "http://localhost:8080/v1/auth/google/callback"

func google(t *testing.T, srv *socialtest.Server) *social.Provider {
	t.Helper()
	p, err := social.NewGoogle(social.GoogleConfig{
		ClientID: "web.apps.googleusercontent.com", ClientSecret: "GOCSPX-secret",
		NativeClientIDs: []string{"ios.apps.googleusercontent.com"}, Endpoints: srv.Endpoints(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func appleKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func apple(t *testing.T, srv *socialtest.Server, key *ecdsa.PrivateKey) *social.Provider {
	t.Helper()
	p, err := social.NewApple(social.AppleConfig{
		TeamID: "TEAM123456", KeyID: "KEY1234567", PrivateKey: key,
		ServicesID: "com.example.web", BundleIDs: []string{"com.example.app"}, Endpoints: srv.Endpoints(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGoogleWebSignIn(t *testing.T) {
	srv := socialtest.New(t)
	p := google(t, srv)
	verifier := social.NewPKCEVerifier()

	u, err := url.Parse(p.AuthCodeURL(redirect, "state-1", "nonce-1", verifier))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for key, want := range map[string]string{
		"client_id": "web.apps.googleusercontent.com", "redirect_uri": redirect, "response_type": "code",
		"scope": "openid email profile", "state": "state-1", "nonce": "nonce-1", "code_challenge_method": "S256",
	} {
		if q.Get(key) != want {
			t.Errorf("AuthCodeURL() %s = %q, want %q", key, q.Get(key), want)
		}
	}
	if q.Get("code_challenge") == "" || q.Get("response_mode") != "" {
		t.Errorf("AuthCodeURL() = %s, want a PKCE challenge and no form_post", u)
	}

	code := srv.Code(socialtest.Claims{
		Subject: "g-123", Audience: "web.apps.googleusercontent.com", Email: "ada@example.com", EmailVerified: true, Nonce: "nonce-1", Name: "Ada",
	}, "")
	tok, err := p.Exchange(context.Background(), redirect, code, verifier, "nonce-1")
	want := social.Identity{Provider: social.Google, Subject: "g-123", Email: "ada@example.com", EmailVerified: true, Name: "Ada", Audience: "web.apps.googleusercontent.com"}
	if err != nil || tok.Identity != want {
		t.Fatalf("Exchange() = %+v, %v; want %+v", tok, err, want)
	}
	form := srv.TokenRequests()[0]
	if form.Get("code_verifier") != verifier || form.Get("client_secret") != "GOCSPX-secret" || form.Get("redirect_uri") != redirect {
		t.Errorf("token request = %v, want the verifier, secret and redirect URI", form)
	}

	if _, err := p.Exchange(context.Background(), redirect, code, verifier, "nonce-1"); !errors.Is(err, social.ErrExchange) {
		t.Errorf("Exchange(used code) error = %v, want ErrExchange", err)
	}
	other := srv.Code(socialtest.Claims{Subject: "g-123", Audience: "web.apps.googleusercontent.com", Nonce: "nonce-2"}, "")
	if _, err := p.Exchange(context.Background(), redirect, other, verifier, "nonce-1"); !errors.Is(err, social.ErrInvalidToken) {
		t.Errorf("Exchange(other nonce) error = %v, want ErrInvalidToken", err)
	}
}

func TestVerifyIDToken(t *testing.T) {
	srv := socialtest.New(t)
	p := google(t, srv)
	ctx := context.Background()
	base := socialtest.Claims{Subject: "g-1", Audience: "ios.apps.googleusercontent.com", Email: "ada@example.com", EmailVerified: true, Nonce: "n"}

	if id, err := p.VerifyIDToken(ctx, srv.IDToken(base), "n"); err != nil || id.Audience != "ios.apps.googleusercontent.com" {
		t.Fatalf("VerifyIDToken(iOS client) = %+v, %v", id, err)
	}
	withExtra := base
	withExtra.Extra = map[string]any{"email_verified": "true"}
	if id, err := p.VerifyIDToken(ctx, srv.IDToken(withExtra), "n"); err != nil || !id.EmailVerified {
		t.Errorf("VerifyIDToken(email_verified as a string) = %+v, %v", id, err)
	}

	tests := []struct {
		name  string
		token func() string
		nonce string
	}{
		{"another client", func() string { c := base; c.Audience = "someone-else"; return srv.IDToken(c) }, "n"},
		{"wrong nonce", func() string { return srv.IDToken(base) }, "other"},
		{"no nonce", func() string { return srv.IDToken(base) }, ""},
		{"another issuer", func() string { c := base; c.Issuer = "https://evil.example"; return srv.IDToken(c) }, "n"},
		{"too old", func() string {
			c := base
			c.IssuedAt = time.Now().Add(-social.MaxTokenAge - time.Minute)
			return srv.IDToken(c)
		}, "n"},
		{"expired", func() string {
			c := base
			c.Extra = map[string]any{"exp": time.Now().Add(-2 * time.Minute).Unix()}
			return srv.IDToken(c)
		}, "n"},
		{"no subject", func() string { c := base; c.Subject = ""; return srv.IDToken(c) }, "n"},
		{"tampered", func() string { tok := srv.IDToken(base); return tok[:len(tok)-4] + "abcd" }, "n"},
		{"signed by another key", func() string { return foreignToken(t, base) }, "n"},
	}
	for _, tt := range tests {
		if _, err := p.VerifyIDToken(ctx, tt.token(), tt.nonce); !errors.Is(err, social.ErrInvalidToken) {
			t.Errorf("%s: VerifyIDToken() error = %v, want ErrInvalidToken", tt.name, err)
		}
	}
}

// foreignToken signs c with a key the server doesn't publish.
func foreignToken(t *testing.T, c socialtest.Claims) string {
	t.Helper()
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "socialtest"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"iss": "x", "sub": c.Subject, "aud": c.Audience, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(), "nonce": c.Nonce})
	obj, _ := signer.Sign(payload)
	tok, _ := obj.CompactSerialize()
	return tok
}

func TestAppleSignIn(t *testing.T) {
	srv := socialtest.New(t)
	key := appleKey(t)
	p := apple(t, srv, key)
	ctx := context.Background()

	u, _ := url.Parse(p.AuthCodeURL("https://api.example.com/v1/auth/apple/callback", "s", "n", social.NewPKCEVerifier()))
	if q := u.Query(); q.Get("response_mode") != "form_post" || q.Get("scope") != "name email" || q.Get("client_id") != "com.example.web" || q.Get("code_challenge") != "" {
		t.Errorf("AuthCodeURL() = %s, want form_post, name and email, the Services ID and no PKCE", u)
	}

	code := srv.Code(socialtest.Claims{
		Subject: "001.apple", Audience: "com.example.web", Email: "x1@privaterelay.appleid.com", Nonce: "n",
		Extra: map[string]any{"email_verified": "true", "is_private_email": "true"},
	}, "refresh-1")
	tok, err := p.Exchange(ctx, "https://api.example.com/v1/auth/apple/callback", code, "", "n")
	if err != nil || tok.RefreshToken != "refresh-1" || !tok.Identity.EmailVerified || !tok.Identity.PrivateEmail {
		t.Fatalf("Exchange() = %+v, %v", tok, err)
	}
	checkClientSecret(t, srv.TokenRequests()[0].Get("client_secret"), key, "com.example.web")

	// iOS: the ID token is for the bundle ID; its code gives a refresh token.
	if _, err := p.VerifyIDToken(ctx, srv.IDToken(socialtest.Claims{Subject: "001.apple", Audience: "com.example.app", Nonce: "h"}), "h"); err != nil {
		t.Errorf("VerifyIDToken(bundle ID) error = %v", err)
	}
	native := srv.Code(socialtest.Claims{Subject: "001.apple", Audience: "com.example.app"}, "refresh-2")
	if refresh, err := p.ExchangeNativeCode(ctx, native, "com.example.app"); err != nil || refresh != "refresh-2" {
		t.Errorf("ExchangeNativeCode() = %q, %v", refresh, err)
	}
	checkClientSecret(t, srv.TokenRequests()[1].Get("client_secret"), key, "com.example.app")
	if _, err := p.ExchangeNativeCode(ctx, native, "com.other.app"); !errors.Is(err, social.ErrInvalidConfig) {
		t.Errorf("ExchangeNativeCode(unknown client) error = %v", err)
	}

	if err := p.Revoke(ctx, "refresh-1", "com.example.web"); err != nil || len(srv.Revoked()) != 1 || srv.Revoked()[0] != "refresh-1" {
		t.Errorf("Revoke() = %v, revoked %v", err, srv.Revoked())
	}

	n, err := p.AppleNotification(ctx, srv.Notification("com.example.app", social.NotificationConsentRevoked, "001.apple"))
	if err != nil || n.Type != social.NotificationConsentRevoked || n.Subject != "001.apple" || n.At.IsZero() {
		t.Errorf("AppleNotification() = %+v, %v", n, err)
	}
	if _, err := p.AppleNotification(ctx, srv.Notification("com.other.app", social.NotificationAccountDelete, "001.apple")); !errors.Is(err, social.ErrInvalidToken) {
		t.Errorf("AppleNotification(other audience) error = %v", err)
	}
	if _, err := p.AppleNotification(ctx, "not-a-jwt"); !errors.Is(err, social.ErrInvalidToken) {
		t.Errorf("AppleNotification(garbage) error = %v", err)
	}
}

// An old notification can't be replayed later, and each carries an ID to
// refuse replays within the window (security review AUTH-M-3).
func TestAppleNotificationFreshness(t *testing.T) {
	ctx := context.Background()
	srv := socialtest.New(t)
	p := apple(t, srv, appleKey(t))

	fresh := srv.Notification("com.example.app", social.NotificationConsentRevoked, "001.apple")
	n, err := p.AppleNotification(ctx, fresh)
	if err != nil || n.ID == "" || n.IssuedAt.IsZero() {
		t.Fatalf("AppleNotification(fresh) = %+v, %v; want an ID and issue time", n, err)
	}
	again, err := p.AppleNotification(ctx, fresh)
	if err != nil || again.ID != n.ID {
		t.Errorf("AppleNotification(same payload) = %+v, %v; want the same ID", again, err)
	}
	if other, _ := p.AppleNotification(ctx, srv.Notification("com.example.app", social.NotificationConsentRevoked, "001.apple")); other.ID == n.ID {
		t.Error("two notifications share an ID")
	}

	for name, at := range map[string]time.Time{
		"a year old":    time.Now().AddDate(-1, 0, 0),
		"two hours old": time.Now().Add(-2 * time.Hour),
		"in the future": time.Now().Add(10 * time.Minute),
	} {
		srv.Now = func() time.Time { return at }
		if _, err := p.AppleNotification(ctx, srv.Notification("com.example.app", social.NotificationAccountDelete, "001.apple")); !errors.Is(err, social.ErrInvalidToken) {
			t.Errorf("AppleNotification(%s) error = %v, want ErrInvalidToken", name, err)
		}
	}
}

// Only an email the provider manages proves the person still owns it
// (security review AUTH-M-1).
func TestAuthoritativeEmail(t *testing.T) {
	srv := socialtest.New(t)
	p := google(t, srv)
	ctx := context.Background()
	verify := func(email string, extra map[string]any) social.Identity {
		t.Helper()
		id, err := p.VerifyIDToken(ctx, srv.IDToken(socialtest.Claims{Subject: "g1", Audience: "web.apps.googleusercontent.com", Email: email, EmailVerified: true, Nonce: "n", Extra: extra}), "n")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	if id := verify("ada@corp.example", map[string]any{"hd": "corp.example"}); id.HostedDomain != "corp.example" || !id.AuthoritativeEmail() {
		t.Errorf("Workspace identity = %+v, authoritative %t; want the hosted domain and true", id, id.AuthoritativeEmail())
	}
	for email, extra := range map[string]map[string]any{
		"ada@corp.example":  nil,
		"ada@other.example": {"hd": "corp.example"},
	} {
		if id := verify(email, extra); id.AuthoritativeEmail() {
			t.Errorf("Google identity %s with %v is authoritative, want not", email, extra)
		}
	}
	for _, id := range []social.Identity{
		{Provider: social.Google, Email: "ada@gmail.com", EmailVerified: true},
		{Provider: social.Google, Email: "ada@GoogleMail.com", EmailVerified: true},
		{Provider: social.Apple, Email: "x1@privaterelay.appleid.com", EmailVerified: true, PrivateEmail: true},
		{Provider: social.Apple, Email: "ada@icloud.com", EmailVerified: true},
		{Provider: social.Apple, Email: "ada@me.com", EmailVerified: true},
	} {
		if !id.AuthoritativeEmail() {
			t.Errorf("%+v isn't authoritative, want it", id)
		}
	}
	for _, id := range []social.Identity{
		{Provider: social.Google, Email: "ada@gmail.com"},
		{Provider: social.Apple, Email: "ada@corp.example", EmailVerified: true},
		{Provider: social.Google, Email: "not-an-address", EmailVerified: true},
		{Provider: "other", Email: "ada@gmail.com", EmailVerified: true},
	} {
		if id.AuthoritativeEmail() {
			t.Errorf("%+v is authoritative, want not", id)
		}
	}
}

// checkClientSecret verifies an Apple client secret JWT.
func checkClientSecret(t *testing.T, secret string, key *ecdsa.PrivateKey, clientID string) {
	t.Helper()
	obj, err := jose.ParseSigned(secret, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("client secret %q: %v", secret, err)
	}
	payload, err := obj.Verify(&key.PublicKey)
	if err != nil {
		t.Fatalf("client secret signature: %v", err)
	}
	var claims map[string]any
	_ = json.Unmarshal(payload, &claims)
	if claims["iss"] != "TEAM123456" || claims["sub"] != clientID || claims["aud"] != "https://appleid.apple.com" || obj.Signatures[0].Header.KeyID != "KEY1234567" {
		t.Errorf("client secret = %v (kid %q)", claims, obj.Signatures[0].Header.KeyID)
	}
}

func TestProviderConfiguration(t *testing.T) {
	key := appleKey(t)
	if _, err := social.NewGoogle(social.GoogleConfig{ClientID: "web"}); !errors.Is(err, social.ErrInvalidConfig) {
		t.Errorf("NewGoogle(no secret) error = %v", err)
	}
	if _, err := social.NewApple(social.AppleConfig{TeamID: "T", KeyID: "K", PrivateKey: key}); !errors.Is(err, social.ErrInvalidConfig) {
		t.Errorf("NewApple(no clients) error = %v", err)
	}
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if _, err := social.NewApple(social.AppleConfig{TeamID: "T", KeyID: "K", PrivateKey: p384, BundleIDs: []string{"a"}}); !errors.Is(err, social.ErrInvalidConfig) {
		t.Errorf("NewApple(P-384 key) error = %v", err)
	}
	native, err := social.NewApple(social.AppleConfig{TeamID: "T", KeyID: "K", PrivateKey: key, BundleIDs: []string{"com.example.app"}})
	if err != nil || native.Web() || native.NativeClients()[0] != "com.example.app" {
		t.Fatalf("NewApple(bundle IDs only) = web %v, %v", native != nil && native.Web(), err)
	}
	if _, err := native.Exchange(context.Background(), "", "code", "", "n"); !errors.Is(err, social.ErrWebUnavailable) {
		t.Errorf("Exchange() without a Services ID error = %v", err)
	}

	der, _ := x509.MarshalPKCS8PrivateKey(key)
	p8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if parsed, err := social.ParseApplePrivateKey(p8); err != nil || !parsed.Equal(key) {
		t.Errorf("ParseApplePrivateKey(.p8) = %v", err)
	}
	rsaKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	rsaDER, _ := x509.MarshalPKCS8PrivateKey(rsaKey)
	for name, in := range map[string][]byte{
		"garbage":     []byte("not a key"),
		"RSA key":     pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rsaDER}),
		"wrong block": []byte(strings.Replace(string(p8), "PRIVATE KEY", "PUBLIC KEY", 2)),
	} {
		if _, err := social.ParseApplePrivateKey(in); !errors.Is(err, social.ErrInvalidConfig) {
			t.Errorf("ParseApplePrivateKey(%s) error = %v", name, err)
		}
	}
}
