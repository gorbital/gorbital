package jwt

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

// FuzzVerify sends mutated tokens and headers to a warm authenticator: it
// never panics, returns only its own errors, and accepts only tokens whose
// claims are exactly those the provider signed.
func FuzzVerify(f *testing.F) {
	testKeys()
	c := newClock()
	p := newIDP(f, publicJWK(rsaKey, "rsa-1", "RS256"), publicJWK(ecKey, "ec-1", "ES256"), publicJWK(edKey, "ed-1", "EdDSA"))
	a := newAuthenticator(f, p, c, Config{})
	signed := claims(c.Now())
	valid := []string{
		sign(f, rsaKey, "rsa-1", jose.RS256, signed),
		sign(f, ecKey, "ec-1", jose.ES256, signed),
		sign(f, edKey, "ed-1", jose.EdDSA, signed),
	}
	enc := base64.RawURLEncoding.EncodeToString
	for _, v := range valid {
		f.Add(v)
		parts := strings.Split(v, ".")
		f.Add(enc([]byte(`{"alg":"none"}`)) + "." + parts[1] + ".")
		f.Add(enc([]byte(`{"alg":"HS256","kid":"rsa-1"}`)) + "." + parts[1] + "." + parts[2])
		f.Add(enc([]byte(`{"alg":"RS256","kid":"rsa-1","crit":["exp"],"exp":1}`)) + "." + parts[1] + "." + parts[2])
		f.Add(enc([]byte(`{"alg":"RS256","jwk":{"kty":"oct","k":"AAAA"}}`)) + "." + parts[1] + "." + parts[2])
		f.Add(parts[0] + "." + parts[1] + "=." + parts[2])
		f.Add(parts[0] + ".." + parts[2])
	}
	f.Add("")
	f.Add("..")
	f.Add("a.b.c.d.e")
	f.Add(`{"payload":"e30","signatures":[]}`)
	f.Fuzz(func(t *testing.T, token string) {
		got, err := a.Verify(context.Background(), token)
		if err != nil {
			if !errors.Is(err, ErrInvalidToken) && !errors.Is(err, ErrKeysUnavailable) {
				t.Fatalf("Verify() error = %v, want ErrInvalidToken or ErrKeysUnavailable", err)
			}
			return
		}
		if got.Subject != signed["sub"] || got.Issuer != testIssuer || !got.ExpiresAt.Equal(c.Now().Add(3600e9)) ||
			strings.Join(got.Strings("permissions"), ",") != "books.book.read,books.book.write" {
			t.Fatalf("Verify(%q) accepted claims the provider didn't sign: %+v", token, got)
		}
	})
}

// FuzzBearerToken checks the Authorization header parser never panics and
// returns trimmed, non-empty tokens of the Bearer scheme only.
func FuzzBearerToken(f *testing.F) {
	for _, s := range []string{"Bearer abc.def.ghi", "bearer  x ", "Basic dXNlcg==", "Bearer", "Bearer ", "BEARER\ta.b.c", "Bearer a b", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, header string) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header["Authorization"] = []string{header}
		token, ok := bearerToken(r)
		if !ok {
			return
		}
		if token == "" || token != strings.TrimSpace(token) || !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			t.Fatalf("bearerToken(%q) = %q", header, token)
		}
	})
}
