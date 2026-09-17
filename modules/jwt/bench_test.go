package jwt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

// BenchmarkVerify measures verification with a warm key cache.
func BenchmarkVerify(b *testing.B) {
	testKeys()
	c := newClock()
	p := newIDP(b, publicJWK(rsaKey, "rsa-1", "RS256"), publicJWK(ecKey, "ec-1", "ES256"), publicJWK(edKey, "ed-1", "EdDSA"))
	a := newAuthenticator(b, p, c, Config{})
	for _, bm := range []struct {
		name  string
		token string
	}{
		{"RS256", sign(b, rsaKey, "rsa-1", jose.RS256, claims(c.Now()))},
		{"ES256", sign(b, ecKey, "ec-1", jose.ES256, claims(c.Now()))},
		{"EdDSA", sign(b, edKey, "ed-1", jose.EdDSA, claims(c.Now()))},
	} {
		b.Run(bm.name, func(b *testing.B) {
			ctx := context.Background()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := a.Verify(ctx, bm.token); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkMiddleware measures an authenticated request through the
// middleware with a warm key cache.
func BenchmarkMiddleware(b *testing.B) {
	testKeys()
	c := newClock()
	p := newIDP(b, publicJWK(rsaKey, "rsa-1", "RS256"))
	a := newAuthenticator(b, p, c, Config{})
	h := a.Middleware(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/v1/books", nil)
	req.Header.Set("Authorization", "Bearer "+sign(b, rsaKey, "rsa-1", jose.RS256, claims(c.Now())))
	w := httptest.NewRecorder()
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(w, req)
	}
}
