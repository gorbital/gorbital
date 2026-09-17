package jwt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"gorbital.dev/actor"
	"gorbital.dev/httpx"
)

func newAuthenticator(t testing.TB, p *idp, c *clock, cfg Config, opts ...Option) *Authenticator {
	t.Helper()
	if cfg.Issuer == "" {
		cfg.Issuer = testIssuer
	}
	if cfg.Audiences == nil {
		cfg.Audiences = []string{testAudience}
	}
	if cfg.JWKSURL == "" && p != nil {
		cfg.JWKSURL = p.url()
	}
	a, err := New(context.Background(), cfg, append([]Option{WithClock(c.Now)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestVerify(t *testing.T) {
	testKeys()
	c := newClock()
	now := c.Now()
	p := newIDP(t,
		publicJWK(rsaKey, "rsa-1", "RS256"),
		publicJWK(ecKey, "ec-1", "ES256"),
		publicJWK(edKey, "ed-1", "EdDSA"),
		publicJWK(ec384Key, "ec384", ""),
	)
	a := newAuthenticator(t, p, c, Config{Algorithms: []string{"RS256", "PS256", "ES256", "ES384", "EdDSA"}})

	with := func(change func(map[string]any)) map[string]any {
		m := claims(now)
		change(m)
		return m
	}
	valid := sign(t, rsaKey, "rsa-1", jose.RS256, claims(now))
	parts := strings.Split(valid, ".")
	tamperedPayload, _ := json.Marshal(with(func(m map[string]any) { m["sub"] = "auth0|admin" }))
	tampered := parts[0] + "." + base64.RawURLEncoding.EncodeToString(tamperedPayload) + "." + parts[2]
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	hsWithPublicKey := func() string {
		// Algorithm confusion: HS256 keyed with the RSA public key's bytes.
		der, _ := json.Marshal(publicJWK(rsaKey, "rsa-1", ""))
		return sign(t, der, "rsa-1", jose.HS256, claims(now))
	}()

	tests := []struct {
		name   string
		token  string
		reason string // in the error; empty means valid
	}{
		{"RS256", valid, ""},
		{"PS256 with an RSA key without alg", sign(t, rsaKey, "rsa-1", jose.PS256, claims(now)), "no key"},
		{"ES256", sign(t, ecKey, "ec-1", jose.ES256, claims(now)), ""},
		{"ES384 on a P-384 key without alg", sign(t, ec384Key, "ec384", jose.ES384, claims(now)), ""},
		{"EdDSA", sign(t, edKey, "ed-1", jose.EdDSA, claims(now)), ""},
		{"audience as a string", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["aud"] = testAudience })), ""},
		{"within the clock skew after expiry", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["exp"] = now.Add(-20 * time.Second).Unix() })), ""},
		{"no nbf or iat", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { delete(m, "nbf"); delete(m, "iat") })), ""},

		{"expired", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["exp"] = now.Add(-time.Minute).Unix() })), "expired"},
		{"not valid yet", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["nbf"] = now.Add(time.Minute).Unix() })), "not valid yet"},
		{"issued in the future", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["iat"] = now.Add(time.Minute).Unix() })), "issued in the future"},
		{"no expiry", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { delete(m, "exp") })), "no expiry"},
		{"wrong issuer", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["iss"] = "https://evil.example.com/" })), "wrong issuer"},
		{"issuer without trailing slash", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["iss"] = strings.TrimSuffix(testIssuer, "/") })), "wrong issuer"},
		{"wrong audience", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["aud"] = []string{"https://other-api.example.com"} })), "wrong audience"},
		{"no audience", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { delete(m, "aud") })), "wrong audience"},
		{"no subject", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { delete(m, "sub") })), "no subject"},
		{"issuer not a string", sign(t, rsaKey, "rsa-1", jose.RS256, with(func(m map[string]any) { m["iss"] = 7 })), "invalid token"},
		{"tampered payload", tampered, "invalid token"},
		{"signed with another key under a known kid", sign(t, rsaKey2, "rsa-1", jose.RS256, claims(now)), "invalid token"},
		{"wrong algorithm for the key", sign(t, rsaKey, "ec-1", jose.RS256, claims(now)), "no key"},
		{"ES256 token on an RS256 kid", sign(t, ecKey, "rsa-1", jose.ES256, claims(now)), "no key"},
		{"algorithm not allowed", sign(t, rsaKey, "rsa-1", jose.RS512, claims(now)), "unexpected signature algorithm"},
		{"alg none", noneHeader + "." + parts[1] + ".", "invalid token"},
		{"HS256 with the public key as secret", hsWithPublicKey, "unexpected signature algorithm"},
		{"no kid, one key for the algorithm", sign(t, rsaKey, "", jose.RS256, claims(now)), ""},
		{"not a JWT", "not-a-token", "invalid token"},
		{"JWE shape", "a.b.c.d.e", "invalid token"},
		{"too long", strings.Repeat("a", MaxTokenBytes+1), "longer than"},
		{"empty", "", "invalid token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.Verify(context.Background(), tt.token)
			if tt.reason == "" {
				if err != nil || got.Subject != "auth0|usr_1" {
					t.Fatalf("Verify() = %+v, %v; want valid", got, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidToken) || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("Verify() error = %v, want ErrInvalidToken with %q", err, tt.reason)
			}
		})
	}
}

func TestVerifyReturnsClaims(t *testing.T) {
	c := newClock()
	now := c.Now()
	p := newIDP(t, publicJWK(ecKey, "ec-1", "ES256"))
	a := newAuthenticator(t, p, c, Config{})
	m := claims(now)
	m["jti"] = "tok_1"
	m["scope"] = "openid read:books  write:books"
	m["org"] = map[string]any{"id": "org_1", "role": "admin"}
	m["email"] = "ada@example.com"
	got, err := a.Verify(context.Background(), sign(t, ecKey, "ec-1", jose.ES256, m))
	if err != nil {
		t.Fatal(err)
	}
	if got.Issuer != testIssuer || got.ID != "tok_1" || len(got.Audience) != 2 || !got.ExpiresAt.Equal(now.Add(time.Hour)) || !got.IssuedAt.Equal(now) || !got.NotBefore.Equal(now) {
		t.Errorf("registered claims = %+v", got)
	}
	if s := got.Strings("scope"); strings.Join(s, ",") != "openid,read:books,write:books" {
		t.Errorf("Strings(scope) = %q", s)
	}
	if s := got.Strings("permissions"); len(s) != 2 {
		t.Errorf("Strings(permissions) = %q", s)
	}
	if got.Strings("org") != nil || got.Strings("missing") != nil || got.String("org") != "" || got.String("email") != "ada@example.com" {
		t.Error("String/Strings of other types or missing claims")
	}
	var org struct{ ID, Role string }
	if err := got.Decode("org", &org); err != nil || org.ID != "org_1" {
		t.Errorf("Decode(org) = %+v, %v", org, err)
	}
	if err := got.Decode("missing", &org); !errors.Is(err, ErrNoClaim) {
		t.Errorf("Decode(missing) error = %v", err)
	}
	if err := got.Decode("email", &org); err == nil || errors.Is(err, ErrNoClaim) {
		t.Errorf("Decode(email into a struct) error = %v", err)
	}
}

func TestAudienceClaim(t *testing.T) {
	c := newClock()
	p := newIDP(t, publicJWK(rsaKey, "rsa-1", "RS256"))
	a := newAuthenticator(t, p, c, Config{Audiences: []string{"cognito-client-1"}, AudienceClaim: "client_id"})
	m := claims(c.Now())
	delete(m, "aud")
	m["client_id"] = "cognito-client-1"
	if _, err := a.Verify(context.Background(), sign(t, rsaKey, "rsa-1", jose.RS256, m)); err != nil {
		t.Errorf("Verify(client_id) error = %v", err)
	}
	m["client_id"] = "cognito-client-2"
	if _, err := a.Verify(context.Background(), sign(t, rsaKey, "rsa-1", jose.RS256, m)); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Verify(other client_id) error = %v", err)
	}
}

func TestHMAC(t *testing.T) {
	c := newClock()
	secret := []byte("0123456789abcdef0123456789abcdef")
	a := newAuthenticator(t, nil, c, Config{Algorithms: []string{"HS256"}}, WithHMACSecret(secret))
	if _, err := a.Verify(context.Background(), sign(t, secret, "", jose.HS256, claims(c.Now()))); err != nil {
		t.Errorf("Verify(HS256) error = %v", err)
	}
	if _, err := a.Verify(context.Background(), sign(t, []byte("fedcba9876543210fedcba9876543210"), "", jose.HS256, claims(c.Now()))); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Verify(HS256 with another secret) error = %v", err)
	}
	testKeys()
	if _, err := a.Verify(context.Background(), sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now()))); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Verify(RS256 on an HS-only authenticator) error = %v", err)
	}
}

func TestNewValidates(t *testing.T) {
	testKeys()
	p := newIDP(t, publicJWK(rsaKey, "rsa-1", "RS256"))
	secret := []byte("0123456789abcdef0123456789abcdef")
	tests := []struct {
		name string
		cfg  Config
		opts []Option
	}{
		{"no issuer", Config{Audiences: []string{testAudience}, JWKSURL: p.url()}, nil},
		{"no audience", Config{Issuer: testIssuer, JWKSURL: p.url()}, nil},
		{"empty audience", Config{Issuer: testIssuer, Audiences: []string{""}, JWKSURL: p.url()}, nil},
		{"no JWKS URL", Config{Issuer: testIssuer, Audiences: []string{testAudience}}, nil},
		{"alg none", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: p.url(), Algorithms: []string{"none"}}, nil},
		{"unknown alg", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: p.url(), Algorithms: []string{"RS1"}}, nil},
		{"HS without secret", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: p.url(), Algorithms: []string{"RS256", "HS256"}}, nil},
		{"short HS secret", Config{Issuer: testIssuer, Audiences: []string{testAudience}, Algorithms: []string{"HS256"}}, []Option{WithHMACSecret([]byte("short"))}},
		{"secret without HS", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: p.url()}, []Option{WithHMACSecret(secret)}},
		{"JWKS URL with HS only", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: p.url(), Algorithms: []string{"HS256"}}, []Option{WithHMACSecret(secret)}},
		{"plain http to a remote host", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: "http://idp.example.com/jwks.json"}, nil},
		{"user info in URL", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: "https://user:pass@idp.example.com/jwks.json"}, nil},
		{"negative skew", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: p.url(), ClockSkew: -time.Second}, nil},
		{"skew too large", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: p.url(), ClockSkew: 6 * time.Minute}, nil},
		{"JWKS unreachable", Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: "http://127.0.0.1:1/jwks.json"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if a, err := New(context.Background(), tt.cfg, tt.opts...); err == nil || a != nil {
				t.Errorf("New() = %v, %v; want an error", a, err)
			}
		})
	}
}

func TestJWKSContents(t *testing.T) {
	testKeys()
	c := newClock()
	tests := []struct {
		name    string
		raw     string
		keys    []jose.JSONWebKey
		wantErr bool
	}{
		{"weak RSA key only", "", []jose.JSONWebKey{publicJWK(rsaWeakKey, "weak", "RS256")}, true},
		{"private key only", "", []jose.JSONWebKey{{Key: rsaKey, KeyID: "priv", Algorithm: "RS256"}}, true},
		{"encryption key only", "", []jose.JSONWebKey{{Key: rsaKey.Public(), KeyID: "enc", Use: "enc"}}, true},
		{"symmetric key only", "", []jose.JSONWebKey{{Key: []byte("0123456789abcdef0123456789abcdef"), KeyID: "oct"}}, true},
		{"not JSON", "<html>", nil, true},
		{"no keys", `{"keys":[]}`, nil, true},
		{"unknown key type next to a good key", `{"keys":[{"kty":"XYZ","kid":"x"},` + mustJSON(publicJWK(rsaKey, "rsa-1", "RS256")) + `]}`, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newIDP(t, tt.keys...)
			p.raw = tt.raw
			_, err := New(context.Background(), Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: p.url()}, WithClock(c.Now))
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, want error %t", err, tt.wantErr)
			}
		})
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestKeyRotation(t *testing.T) {
	testKeys()
	c := newClock()
	p := newIDP(t, publicJWK(rsaKey, "rsa-1", "RS256"))
	a := newAuthenticator(t, p, c, Config{})
	ctx := context.Background()
	if p.fetches.Load() != 1 {
		t.Fatalf("fetches after New = %d", p.fetches.Load())
	}

	// The provider rotates to a new key. A token under the new kid, right
	// after start, is refused without a fetch (rate limit)…
	p.setKeys(publicJWK(rsaKey, "rsa-1", "RS256"), publicJWK(rsaKey2, "rsa-2", "RS256"))
	rotated := func() string { return sign(t, rsaKey2, "rsa-2", jose.RS256, claims(c.Now())) }
	if _, err := a.Verify(ctx, rotated()); !errors.Is(err, ErrInvalidToken) || p.fetches.Load() != 1 {
		t.Fatalf("Verify(new kid within the refetch interval) error = %v, fetches %d", err, p.fetches.Load())
	}
	// …and accepted once the interval has passed, after one fetch.
	c.Add(minRefetchInterval)
	if _, err := a.Verify(ctx, rotated()); err != nil || p.fetches.Load() != 2 {
		t.Fatalf("Verify(new kid) error = %v, fetches %d", err, p.fetches.Load())
	}
	// Without a kid, two keys for the algorithm are ambiguous.
	if _, err := a.Verify(ctx, sign(t, rsaKey, "", jose.RS256, claims(c.Now()))); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Verify(no kid, two RSA keys) error = %v", err)
	}
	// Known keys don't fetch.
	for range 10 {
		if _, err := a.Verify(ctx, sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now()))); err != nil {
			t.Fatal(err)
		}
	}
	if p.fetches.Load() != 2 {
		t.Errorf("fetches for known keys = %d", p.fetches.Load())
	}

	// Unknown kids don't cause a fetch storm: concurrent requests with
	// random kids share at most one fetch per interval.
	c.Add(minRefetchInterval)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			_, err := a.Verify(ctx, sign(t, rsaKey2, "unknown-"+string(rune('a'+i%26)), jose.RS256, claims(c.Now())))
			if !errors.Is(err, ErrInvalidToken) {
				t.Errorf("Verify(unknown kid) error = %v", err)
			}
		})
	}
	wg.Wait()
	if got := p.fetches.Load(); got != 3 {
		t.Errorf("fetches after 50 unknown kids = %d, want 3", got)
	}

	// The old key is removed: after the cache expires, its tokens fail.
	p.setKeys(publicJWK(rsaKey2, "rsa-2", "RS256"))
	c.Add(defaultCacheAge)
	if _, err := a.Verify(ctx, sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now()))); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("Verify(removed key) error = %v", err)
	}
	if _, err := a.Verify(ctx, rotated()); err != nil {
		t.Errorf("Verify(current key) error = %v", err)
	}
}

func TestJWKSDown(t *testing.T) {
	testKeys()
	c := newClock()
	p := newIDP(t, publicJWK(rsaKey, "rsa-1", "RS256"))
	var logs bytes.Buffer
	a := newAuthenticator(t, p, c, Config{})
	a.Middleware(slog.New(slog.NewTextHandler(&logs, nil)))
	ctx := context.Background()
	p.setDown(true)

	// Cached keys keep working after the cache age while the provider is
	// down, up to maxStale.
	c.Add(defaultCacheAge + time.Minute)
	if _, err := a.Verify(ctx, sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now()))); err != nil {
		t.Errorf("Verify(cached key, provider down) error = %v", err)
	}
	if !strings.Contains(logs.String(), "fetch JWKS") {
		t.Errorf("failed fetch not logged: %q", logs.String())
	}
	// A key that isn't cached can't be verified: unavailable, not invalid.
	c.Add(minRefetchInterval)
	if _, err := a.Verify(ctx, sign(t, rsaKey2, "rsa-2", jose.RS256, claims(c.Now()))); !errors.Is(err, ErrKeysUnavailable) {
		t.Errorf("Verify(new kid, provider down) error = %v, want ErrKeysUnavailable", err)
	}
	// Retries are rate limited while down.
	before := p.fetches.Load()
	for range 5 {
		_, _ = a.Verify(ctx, sign(t, rsaKey2, "rsa-2", jose.RS256, claims(c.Now())))
	}
	if p.fetches.Load() != before {
		t.Errorf("fetches while down = %d, want %d", p.fetches.Load(), before)
	}
	// Past maxStale, cached keys are no longer used.
	c.Add(maxStale)
	if _, err := a.Verify(ctx, sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now()))); !errors.Is(err, ErrKeysUnavailable) {
		t.Errorf("Verify(stale key) error = %v, want ErrKeysUnavailable", err)
	}
	// The provider comes back.
	p.setDown(false)
	c.Add(minRefetchInterval)
	if _, err := a.Verify(ctx, sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now()))); err != nil {
		t.Errorf("Verify(after recovery) error = %v", err)
	}
}

func TestSlowJWKSAndCancelledRequest(t *testing.T) {
	testKeys()
	c := newClock()
	release := make(chan struct{})
	var slow sync.Once
	body, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{publicJWK(rsaKey, "rsa-1", "RS256"), publicJWK(rsaKey2, "rsa-2", "RS256")}})
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) > 1 {
			slow.Do(func() { <-release })
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	a, err := New(context.Background(), Config{Issuer: testIssuer, Audiences: []string{testAudience}, JWKSURL: srv.URL}, WithClock(c.Now))
	if err != nil {
		t.Fatal(err)
	}
	c.Add(minRefetchInterval)
	// A request that gives up while the fetch is slow gets unavailable…
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	token := sign(t, rsaKey2, "unknown", jose.RS256, claims(c.Now()))
	if _, err := a.Verify(ctx, token); !errors.Is(err, ErrKeysUnavailable) {
		t.Errorf("Verify(cancelled during fetch) error = %v", err)
	}
	// …and the shared fetch completes for the next one.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.keys.mu.Lock()
		fetching := a.keys.fetching != nil
		a.keys.mu.Unlock()
		if !fetching || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := a.Verify(context.Background(), sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now()))); err != nil {
		t.Errorf("Verify(after the fetch) error = %v", err)
	}
}

func TestCacheAge(t *testing.T) {
	tests := map[string]time.Duration{
		"":                            defaultCacheAge,
		"max-age=3600":                time.Hour,
		"public, max-age=86400, must": 24 * time.Hour,
		"max-age=10":                  minCacheAge,
		"max-age=9999999999999":       maxCacheAge,
		"no-store":                    minCacheAge,
		"no-cache, max-age=3600":      minCacheAge,
		`max-age="7200"`:              2 * time.Hour,
		"max-age=abc":                 defaultCacheAge,
		"max-age=-5":                  defaultCacheAge,
		"private":                     defaultCacheAge,
	}
	for header, want := range tests {
		if got := cacheAge(header); got != want {
			t.Errorf("cacheAge(%q) = %v, want %v", header, got, want)
		}
	}
	// The cache age from the provider is used.
	testKeys()
	c := newClock()
	p := newIDP(t, publicJWK(rsaKey, "rsa-1", "RS256"))
	p.cacheControl = "max-age=600"
	a := newAuthenticator(t, p, c, Config{})
	c.Add(9 * time.Minute)
	_, _ = a.Verify(context.Background(), sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now())))
	if p.fetches.Load() != 1 {
		t.Errorf("fetches within max-age = %d", p.fetches.Load())
	}
	c.Add(2 * time.Minute)
	_, _ = a.Verify(context.Background(), sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now())))
	if p.fetches.Load() != 2 {
		t.Errorf("fetches after max-age = %d", p.fetches.Load())
	}
}

func TestRedirects(t *testing.T) {
	if err := checkRedirect(httptest.NewRequest(http.MethodGet, "http://idp.example.com/jwks", nil), nil); err == nil {
		t.Error("redirect to plain http allowed")
	}
	if err := checkRedirect(httptest.NewRequest(http.MethodGet, "https://idp.example.com/jwks", nil), make([]*http.Request, 3)); err == nil {
		t.Error("fourth redirect allowed")
	}
	if err := checkRedirect(httptest.NewRequest(http.MethodGet, "https://idp.example.com/jwks", nil), nil); err != nil {
		t.Errorf("https redirect refused: %v", err)
	}
}

func TestMiddleware(t *testing.T) {
	testKeys()
	c := newClock()
	now := c.Now()
	p := newIDP(t, publicJWK(rsaKey, "rsa-1", "RS256"))
	a := newAuthenticator(t, p, c, Config{})

	var seen struct {
		actor    actor.Actor
		hasActor bool
		client   actor.Client
	}
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen.actor, seen.hasActor = actor.From(r.Context())
		seen.client, _ = actor.ClientFrom(r.Context())
	})
	var logs bytes.Buffer
	h := a.Middleware(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))(next)

	do := func(authorization string, ctx context.Context) (int, string, string) {
		seen.actor, seen.hasActor, seen.client = actor.Actor{}, false, actor.Client{}
		req := httptest.NewRequest(http.MethodGet, "/v1/books", nil)
		req.RemoteAddr = "203.0.113.7:4444"
		req.Header.Set("User-Agent", "books-ios/2.4")
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		if ctx != nil {
			req = req.WithContext(ctx)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var p struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		return rec.Code, p.Code, rec.Header().Get("WWW-Authenticate")
	}

	valid := sign(t, rsaKey, "rsa-1", jose.RS256, claims(now))
	expiredClaims := claims(now)
	expiredClaims["exp"] = now.Add(-time.Hour).Unix()
	expired := sign(t, rsaKey, "rsa-1", jose.RS256, expiredClaims)

	// A valid token sets a user actor with the token's permissions and
	// the client.
	if status, _, _ := do("Bearer "+valid, nil); status != 200 || !seen.hasActor ||
		seen.actor.Kind != actor.KindUser || seen.actor.ID != "auth0|usr_1" || !seen.actor.Can("books.book.write") ||
		seen.client.IP != "203.0.113.7" || seen.client.UserAgent != "books-ios/2.4" {
		t.Errorf("valid token: status %d, actor %+v %t, client %+v", status, seen.actor, seen.hasActor, seen.client)
	}
	if status, _, _ := do("bearer   "+valid+" ", nil); status != 200 || !seen.hasActor {
		t.Errorf("lower-case scheme and spaces: status %d, actor %t", status, seen.hasActor)
	}

	// No token, another scheme, or a token that isn't a JWT: anonymous.
	for _, authorization := range []string{"", "Basic dXNlcjpwYXNz", "Bearer gbk_live_abc123", "Bearer ", "Bearer"} {
		if status, _, _ := do(authorization, nil); status != 200 || seen.hasActor {
			t.Errorf("Authorization %q: status %d, actor %t; want anonymous", authorization, status, seen.hasActor)
		}
	}

	// A JWT that fails verification: 401 invalid_token, never anonymous.
	logs.Reset()
	for _, token := range []string{expired, valid[:len(valid)-4] + "AAAA", "a.b.c"} {
		status, code, challenge := do("Bearer "+token, nil)
		if status != 401 || code != "invalid_token" || challenge != `Bearer error="invalid_token"` || seen.hasActor {
			t.Errorf("invalid token: %d %s %q, next called %t", status, code, challenge, seen.hasActor)
		}
	}
	if !strings.Contains(logs.String(), "level=DEBUG") || strings.Contains(logs.String(), valid[:20]) {
		t.Errorf("refusals should be logged at debug without the token: %s", logs.String())
	}

	// An actor set by an earlier authenticator is kept.
	session := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_session"})
	if status, _, _ := do("Bearer "+expired, session); status != 200 || seen.actor.ID != "usr_session" {
		t.Errorf("earlier actor: status %d, actor %+v", status, seen.actor)
	}
	// A client set by earlier middleware is kept.
	withClient := actor.WithClient(context.Background(), actor.Client{IP: "198.51.100.1", UserAgent: "proxy"})
	if status, _, _ := do("Bearer "+valid, withClient); status != 200 || seen.client.IP != "198.51.100.1" {
		t.Errorf("earlier client: status %d, client %+v", status, seen.client)
	}

	// Keys unavailable: 503.
	p.setDown(true)
	c.Add(minRefetchInterval)
	if status, code, _ := do("Bearer "+sign(t, rsaKey2, "rsa-2", jose.RS256, claims(c.Now())), nil); status != 503 || code != "auth_unavailable" {
		t.Errorf("keys unavailable: %d %s", status, code)
	}
}

func TestMiddlewareActorFrom(t *testing.T) {
	testKeys()
	c := newClock()
	p := newIDP(t, publicJWK(ecKey, "ec-1", "ES256"))
	errNoOrg := errors.New("no organisation")
	a := newAuthenticator(t, p, c, Config{
		PermissionsClaim: "scope",
		ActorFrom: func(cl Claims) (actor.Actor, error) {
			switch {
			case cl.String("gty") == "client-credentials":
				return actor.Actor{Kind: actor.KindService, ID: cl.Subject, Permissions: cl.Strings("scope")}, nil
			case cl.String("kind") == "system":
				return actor.Actor{Kind: actor.KindSystem, ID: "x"}, nil
			case cl.String("kind") == "no-id":
				return actor.Actor{Kind: actor.KindUser}, nil
			case cl.String("org") == "":
				return actor.Actor{}, errNoOrg
			}
			return actor.Actor{Kind: actor.KindUser, ID: cl.Subject, OrgID: cl.String("org")}, nil
		},
	})
	var got actor.Actor
	h := a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got, _ = actor.From(r.Context()) }))
	for _, tt := range []struct {
		extra      map[string]any
		wantStatus int
		wantKind   actor.Kind
	}{
		{map[string]any{"gty": "client-credentials", "scope": "books.book.read"}, 200, actor.KindService},
		{map[string]any{"org": "org_1"}, 200, actor.KindUser},
		{map[string]any{}, 401, ""},
		{map[string]any{"kind": "system"}, 401, ""},
		{map[string]any{"kind": "no-id"}, 401, ""},
	} {
		got = actor.Actor{}
		m := claims(c.Now())
		for k, v := range tt.extra {
			m[k] = v
		}
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+sign(t, ecKey, "ec-1", jose.ES256, m))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tt.wantStatus || got.Kind != tt.wantKind {
			t.Errorf("claims %v: status %d, actor %+v; want %d %s", tt.extra, rec.Code, got, tt.wantStatus, tt.wantKind)
		}
	}
}

func TestMiddlewareAccessNote(t *testing.T) {
	testKeys()
	c := newClock()
	p := newIDP(t, publicJWK(rsaKey, "rsa-1", "RS256"))
	a := newAuthenticator(t, p, c, Config{})
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	h := httpx.Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), httpx.AccessLog(logger), a.Middleware(logger))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+sign(t, rsaKey, "rsa-1", jose.RS256, claims(c.Now())))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(logs.String(), "user_id=auth0|usr_1") {
		t.Errorf("access log lacks the user: %s", logs.String())
	}
}

// TestAppClientKeepsTheRedirectPolicy: an app's own HTTP client
// (WithHTTPClient), such as one with a proxy or custom roots, still fetches
// the JWKS under the redirect allowlist and the fetch timeout. Without them
// Go's default policy follows up to ten redirects to any scheme and host, so
// one redirect out of the provider's JWKS endpoint would let another server
// supply the keys every token is verified against (internal security review,
// 2026-09, JWT-1).
func TestAppClientKeepsTheRedirectPolicy(t *testing.T) {
	testKeys()
	c := newClock()
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A host that is neither https nor loopback, so the allowlist
		// refuses it; the request is never made.
		http.Redirect(w, r, "http://attacker.example.com/jwks.json", http.StatusFound)
	}))
	defer redirecting.Close()

	cfg := Config{
		Issuer:    "https://idp.example.com/",
		Audiences: []string{"https://api.example.com"},
		JWKSURL:   redirecting.URL + "/.well-known/jwks.json",
	}
	appClient := &http.Client{Timeout: 5 * time.Second}
	for name, client := range map[string]*http.Client{
		"the default client":  nil,
		"an app's own client": appClient,
	} {
		t.Run(name, func(t *testing.T) {
			opts := []Option{WithClock(c.Now)}
			if client != nil {
				opts = append(opts, WithHTTPClient(client))
			}
			_, err := New(context.Background(), cfg, opts...)
			if err == nil {
				t.Fatal("New() followed a JWKS redirect to a plain-http host")
			}
			if !strings.Contains(err.Error(), "refusing a JWKS redirect") {
				t.Errorf("New() error = %v, want the redirect refusal", err)
			}
		})
	}
	if appClient.CheckRedirect != nil || appClient.Timeout != 5*time.Second {
		t.Error("WithHTTPClient's client was modified; keysClient must copy it")
	}
}

// TestKeysClient: keysClient fills in the redirect allowlist and the fetch
// timeout an app's client leaves unset, and keeps the ones it sets.
func TestKeysClient(t *testing.T) {
	refuse := func(*http.Request, []*http.Request) error { return errors.New("app") }
	if got := keysClient(nil); got.CheckRedirect == nil || got.Timeout != fetchTimeout {
		t.Errorf("keysClient(nil) = %+v", got)
	}
	got := keysClient(&http.Client{})
	if got.CheckRedirect == nil || got.Timeout != fetchTimeout {
		t.Errorf("keysClient(empty) = %+v", got)
	}
	if err := got.CheckRedirect(httptest.NewRequest(http.MethodGet, "http://idp.example.com/jwks", nil), nil); err == nil {
		t.Error("the filled-in policy allowed a plain-http redirect")
	}
	got = keysClient(&http.Client{Timeout: time.Second, CheckRedirect: refuse})
	if got.Timeout != time.Second || got.CheckRedirect(nil, nil) == nil {
		t.Error("keysClient replaced the app's own timeout or redirect policy")
	}
}

// TestDefaultActorDropsOpsPermissions: the operations console authorizes on
// the actor's permissions alone, so the default actor mapping must not let a
// provider's claim carry ops.* permissions. Without this, a token whose
// permissions (or client-requested "scope") claim contained
// "ops.settings.write" reached /ops as a platform administrator, and with an
// empty StepUp it skipped the second factor those permissions normally need
// (internal security review, 2026-09, JWT-2).
func TestDefaultActorDropsOpsPermissions(t *testing.T) {
	testKeys()
	c := newClock()
	p := newIDP(t, publicJWK(rsaKey, "rsa-1", "RS256"))
	a := newAuthenticator(t, p, c, Config{})

	cl := claims(c.Now())
	cl["permissions"] = []string{"ops.settings.write", "books.book.read", "ops.audit.read"}
	var got actor.Actor
	h := a.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = actor.From(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/ops/settings", nil)
	req.Header.Set("Authorization", "Bearer "+sign(t, rsaKey, "rsa-1", jose.RS256, cl))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("the token was refused: %d %s", rec.Code, rec.Body)
	}
	if !slices.Equal(got.Permissions, []string{"books.book.read"}) {
		t.Errorf("permissions = %v, want only the app's own", got.Permissions)
	}
	ctx := actor.With(context.Background(), got)
	for _, permission := range []string{"ops.settings.write", "ops.audit.read"} {
		if err := actor.Require(ctx, permission); !errors.Is(err, actor.ErrForbidden) {
			t.Errorf("Require(%q) = %v, want ErrForbidden", permission, err)
		}
	}

	// ActorFrom stays in charge: an app that does grant /ops through its
	// provider says so there.
	cfg := Config{ActorFrom: func(c Claims) (actor.Actor, error) {
		return actor.Actor{Kind: actor.KindUser, ID: c.Subject, Permissions: c.Strings("permissions")}, nil
	}}
	b := newAuthenticator(t, p, c, cfg)
	h = b.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = actor.From(r.Context())
	}))
	req = httptest.NewRequest(http.MethodGet, "/ops/settings", nil)
	req.Header.Set("Authorization", "Bearer "+sign(t, rsaKey, "rsa-1", jose.RS256, cl))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !slices.Contains(got.Permissions, "ops.settings.write") {
		t.Errorf("ActorFrom's permissions = %v, want them kept", got.Permissions)
	}
}
