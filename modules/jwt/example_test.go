package jwt_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"

	"gorbital.dev/actor"
	"gorbital.dev/modules/jwt"
)

// provider stands in for an identity provider: it publishes a JWKS and
// signs tokens.
type provider struct {
	key *ecdsa.PrivateKey
	srv *httptest.Server
}

func newProvider() *provider {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	p := &provider{key: key}
	jwks, _ := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: key.Public(), KeyID: "key-1", Algorithm: "ES256", Use: "sig"}}})
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write(jwks)
	}))
	return p
}

func (p *provider) jwksURL() string { return p.srv.URL + "/.well-known/jwks.json" }

// token signs claims, adding the issuer, audience and a one-hour expiry.
func (p *provider) token(claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: p.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "key-1"))
	if err != nil {
		panic(err)
	}
	all := map[string]any{"iss": "https://idp.example.com/", "aud": "https://api.example.com", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()}
	for k, v := range claims {
		all[k] = v
	}
	token, err := josejwt.Signed(signer).Claims(all).Serialize()
	if err != nil {
		panic(err)
	}
	return token
}

func ExampleNew() {
	idp := newProvider()
	defer idp.srv.Close()

	auth, err := jwt.New(context.Background(), jwt.Config{
		Issuer:    "https://idp.example.com/",
		Audiences: []string{"https://api.example.com"},
		JWKSURL:   idp.jwksURL(),
	})
	fmt.Println(auth != nil, err)

	_, err = jwt.New(context.Background(), jwt.Config{
		Issuer:     "https://idp.example.com/",
		Audiences:  []string{"https://api.example.com"},
		JWKSURL:    idp.jwksURL(),
		Algorithms: []string{"none"},
	})
	fmt.Println(err)
	// Output:
	// true <nil>
	// jwt: algorithm "none" is not supported
}

func ExampleAuthenticator() {
	idp := newProvider()
	defer idp.srv.Close()
	auth, err := jwt.New(context.Background(), jwt.Config{
		Issuer:    "https://idp.example.com/",
		Audiences: []string{"https://api.example.com"},
		JWKSURL:   idp.jwksURL(),
	})
	if err != nil {
		panic(err)
	}
	// In a gorbital app: gorbital.WithAuth(auth). With net/http:
	books := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := actor.Require(r.Context(), "books.book.read"); err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte("[]"))
	})
	handler := auth.Middleware(slog.Default())(books)

	req := httptest.NewRequest(http.MethodGet, "/v1/books", nil)
	req.Header.Set("Authorization", "Bearer "+idp.token(map[string]any{"sub": "usr_1", "permissions": []string{"books.book.read"}}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Body.String())
	// Output: 200 []
}

func ExampleAuthenticator_Middleware() {
	idp := newProvider()
	defer idp.srv.Close()
	auth, err := jwt.New(context.Background(), jwt.Config{
		Issuer:    "https://idp.example.com/",
		Audiences: []string{"https://api.example.com"},
		JWKSURL:   idp.jwksURL(),
	})
	if err != nil {
		panic(err)
	}
	handler := auth.Middleware(slog.Default())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := actor.FromOrAnonymous(r.Context())
		fmt.Fprintf(w, "%s:%s", a.Kind, a.ID)
	}))

	valid := idp.token(map[string]any{"sub": "usr_1"})
	for _, authorization := range []string{
		"Bearer " + valid,
		"", // no token: anonymous
		"Bearer " + valid[:len(valid)-4] + "AAAA", // a tampered JWT: refused
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		fmt.Printf("%d %q %s\n", rec.Code, rec.Header().Get("WWW-Authenticate"), rec.Body.String())
	}
	// Output:
	// 200 "" user:usr_1
	// 200 "" anonymous:
	// 401 "Bearer error=\"invalid_token\"" {"title":"Unauthorized","status":401,"code":"invalid_token","detail":"the access token is invalid or expired; get a new one"}
}

func ExampleAuthenticator_Verify() {
	idp := newProvider()
	defer idp.srv.Close()
	auth, err := jwt.New(context.Background(), jwt.Config{
		Issuer:    "https://idp.example.com/",
		Audiences: []string{"https://api.example.com"},
		JWKSURL:   idp.jwksURL(),
	})
	if err != nil {
		panic(err)
	}
	claims, err := auth.Verify(context.Background(), idp.token(map[string]any{"sub": "usr_1"}))
	fmt.Println(claims.Subject, err)

	expired := idp.token(map[string]any{"sub": "usr_1", "exp": time.Now().Add(-time.Hour).Unix()})
	_, err = auth.Verify(context.Background(), expired)
	fmt.Println(errors.Is(err, jwt.ErrInvalidToken), err)
	// Output:
	// usr_1 <nil>
	// true jwt: invalid token: expired
}

func ExampleConfig() {
	idp := newProvider()
	defer idp.srv.Close()
	auth, err := jwt.New(context.Background(), jwt.Config{
		Issuer:    "https://idp.example.com/",
		Audiences: []string{"https://api.example.com"},
		JWKSURL:   idp.jwksURL(),
		ClockSkew: time.Minute,
		// Machine-to-machine tokens become service actors, and the
		// provider's "read:books" scope becomes the app's permission.
		ActorFrom: func(c jwt.Claims) (actor.Actor, error) {
			kind := actor.KindUser
			if c.String("gty") == "client-credentials" {
				kind = actor.KindService
			}
			a := actor.Actor{Kind: kind, ID: c.Subject}
			for _, scope := range c.Strings("scope") {
				if scope == "read:books" {
					a.Permissions = append(a.Permissions, "books.book.read")
				}
			}
			return a, nil
		},
	})
	if err != nil {
		panic(err)
	}
	handler := auth.Middleware(nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		a, _ := actor.From(r.Context())
		fmt.Println(a.Kind, a.ID, a.Permissions)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+idp.token(map[string]any{"sub": "client_42@clients", "gty": "client-credentials", "scope": "read:books"}))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	// Output: service client_42@clients [books.book.read]
}

func ExampleClaims() {
	idp := newProvider()
	defer idp.srv.Close()
	auth, err := jwt.New(context.Background(), jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()})
	if err != nil {
		panic(err)
	}
	c, err := auth.Verify(context.Background(), idp.token(map[string]any{"sub": "usr_1", "jti": "tok_9"}))
	if err != nil {
		panic(err)
	}
	fmt.Println(c.Issuer, c.Subject, c.Audience, c.ID, time.Until(c.ExpiresAt) > 59*time.Minute)
	// Output: https://idp.example.com/ usr_1 [https://api.example.com] tok_9 true
}

func verified(claims map[string]any) jwt.Claims {
	idp := newProvider()
	defer idp.srv.Close()
	auth, err := jwt.New(context.Background(), jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()})
	if err != nil {
		panic(err)
	}
	c, err := auth.Verify(context.Background(), idp.token(claims))
	if err != nil {
		panic(err)
	}
	return c
}

func ExampleClaims_String() {
	c := verified(map[string]any{"sub": "usr_1", "org_id": "org_7"})
	fmt.Printf("%q %q\n", c.String("org_id"), c.String("missing"))
	// Output: "org_7" ""
}

func ExampleClaims_Strings() {
	c := verified(map[string]any{"sub": "usr_1", "scope": "openid read:books", "permissions": []string{"books.book.read"}})
	fmt.Println(c.Strings("scope"), c.Strings("permissions"))
	// Output: [openid read:books] [books.book.read]
}

func ExampleClaims_Decode() {
	// Supabase puts app metadata in a nested object.
	c := verified(map[string]any{"sub": "usr_1", "app_metadata": map[string]any{"provider": "email", "tenant": "org_7"}})
	var meta struct {
		Tenant string `json:"tenant"`
	}
	err := c.Decode("app_metadata", &meta)
	fmt.Println(meta.Tenant, err)
	fmt.Println(errors.Is(c.Decode("user_metadata", &meta), jwt.ErrNoClaim))
	// Output:
	// org_7 <nil>
	// true
}

func ExampleOption() {
	idp := newProvider()
	defer idp.srv.Close()
	_, err := jwt.New(context.Background(),
		jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()},
		jwt.WithHTTPClient(&http.Client{Timeout: 5 * time.Second}),
		jwt.WithClock(time.Now),
	)
	fmt.Println(err)
	// Output: <nil>
}

func ExampleWithHTTPClient() {
	idp := newProvider()
	defer idp.srv.Close()
	// A client with the proxy, timeout or root certificates the
	// deployment needs.
	client := &http.Client{Timeout: 3 * time.Second, Transport: http.DefaultTransport}
	_, err := jwt.New(context.Background(),
		jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()},
		jwt.WithHTTPClient(client))
	fmt.Println(err)
	// Output: <nil>
}

func ExampleWithClock() {
	idp := newProvider()
	defer idp.srv.Close()
	tomorrow := func() time.Time { return time.Now().Add(24 * time.Hour) }
	auth, err := jwt.New(context.Background(),
		jwt.Config{Issuer: "https://idp.example.com/", Audiences: []string{"https://api.example.com"}, JWKSURL: idp.jwksURL()},
		jwt.WithClock(tomorrow))
	if err != nil {
		panic(err)
	}
	_, err = auth.Verify(context.Background(), idp.token(map[string]any{"sub": "usr_1"}))
	fmt.Println(err)
	// Output: jwt: invalid token: expired
}

func ExampleWithHMACSecret() {
	secret := []byte("a-32-byte-or-longer-project-secret") // from the environment, never in code
	auth, err := jwt.New(context.Background(),
		jwt.Config{Issuer: "https://project.supabase.co/auth/v1", Audiences: []string{"authenticated"}, Algorithms: []string{"HS256"}},
		jwt.WithHMACSecret(secret))
	if err != nil {
		panic(err)
	}
	signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: secret}, nil)
	token, _ := josejwt.Signed(signer).Claims(map[string]any{
		"iss": "https://project.supabase.co/auth/v1", "aud": "authenticated", "sub": "usr_1", "exp": time.Now().Add(time.Hour).Unix(),
	}).Serialize()
	c, err := auth.Verify(context.Background(), token)
	fmt.Println(c.Subject, err)
	// Output: usr_1 <nil>
}
