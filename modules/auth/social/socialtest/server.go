// Package socialtest runs an in-process OpenID Connect provider standing in
// for Google or Apple in tests: it serves signing keys, a token endpoint and
// a revocation endpoint, and issues signed ID tokens, authorization codes and
// Apple notifications. Point a social provider at it with Endpoints.
package socialtest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"apistock.dev/modules/auth/social"
)

const keyID = "socialtest"

// Claims describe an ID token to issue.
type Claims struct {
	Subject       string
	Audience      string
	Email         string
	EmailVerified bool
	PrivateEmail  bool
	Nonce         string
	Name          string
	// IssuedAt defaults to the server's Now; Issuer to its URL.
	IssuedAt time.Time
	Issuer   string
	// Extra adds or replaces claims; a nil value removes one.
	Extra map[string]any
}

// Server is a fake provider. It is safe for concurrent use.
type Server struct {
	// URL is the server's base URL and issuer.
	URL string
	// Now is the clock for issued tokens. Default: time.Now.
	Now func() time.Time

	srv    *httptest.Server
	key    *rsa.PrivateKey
	signer jose.Signer

	mu       sync.Mutex
	grants   map[string]grant
	requests []url.Values
	revoked  []string
}

type grant struct {
	claims       Claims
	refreshToken string
}

// New starts a server, closed when the test ends.
func New(t testing.TB) *Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: keyID}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Now: time.Now, key: key, signer: signer, grants: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /keys", s.serveKeys)
	mux.HandleFunc("POST /token", s.serveToken)
	mux.HandleFunc("POST /revoke", s.serveRevoke)
	s.srv = httptest.NewServer(mux)
	s.URL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

// Endpoints returns the server's endpoints for a social provider.
func (s *Server) Endpoints() social.Endpoints {
	return social.Endpoints{
		AuthURL: s.URL + "/authorize", TokenURL: s.URL + "/token", KeysURL: s.URL + "/keys", RevokeURL: s.URL + "/revoke",
		Issuers: []string{s.URL},
	}
}

// IDToken returns a signed ID token with c's claims, valid for an hour.
func (s *Server) IDToken(c Claims) string {
	iat := c.IssuedAt
	if iat.IsZero() {
		iat = s.Now()
	}
	iss := c.Issuer
	if iss == "" {
		iss = s.URL
	}
	claims := map[string]any{
		"iss": iss, "sub": c.Subject, "aud": c.Audience, "iat": iat.Unix(), "exp": iat.Add(time.Hour).Unix(),
		"email": c.Email, "email_verified": c.EmailVerified,
	}
	if c.Nonce != "" {
		claims["nonce"] = c.Nonce
	}
	if c.Name != "" {
		claims["name"] = c.Name
	}
	if c.PrivateEmail {
		claims["is_private_email"] = true
	}
	for k, v := range c.Extra {
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	return s.sign(claims)
}

// Code returns a single-use authorization code whose exchange returns an ID
// token with c's claims and refreshToken (none when empty).
func (s *Server) Code(c Claims, refreshToken string) string {
	code := random()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[code] = grant{claims: c, refreshToken: refreshToken}
	return code
}

// Notification returns a signed Apple server-to-server notification.
func (s *Server) Notification(audience, eventType, subject string) string {
	events, _ := json.Marshal(map[string]any{"type": eventType, "sub": subject, "event_time": s.Now().UnixMilli()})
	return s.sign(map[string]any{"iss": s.URL, "aud": audience, "iat": s.Now().Unix(), "jti": random(), "events": string(events)})
}

// TokenRequests returns the form of every token request received.
func (s *Server) TokenRequests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.requests...)
}

// Revoked returns every token revoked.
func (s *Server) Revoked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.revoked...)
}

func (s *Server) sign(claims map[string]any) string {
	payload, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	obj, err := s.signer.Sign(payload)
	if err != nil {
		panic(err)
	}
	token, err := obj.CompactSerialize()
	if err != nil {
		panic(err)
	}
	return token
}

func (s *Server) serveKeys(w http.ResponseWriter, _ *http.Request) {
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &s.key.PublicKey, KeyID: keyID, Algorithm: string(jose.RS256), Use: "sig"}}}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(set)
}

func (s *Server) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	code := r.PostForm.Get("code")
	s.mu.Lock()
	s.requests = append(s.requests, r.PostForm)
	g, ok := s.grants[code]
	delete(s.grants, code)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if !ok || r.PostForm.Get("client_secret") == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
		return
	}
	resp := map[string]any{"access_token": "access-" + code, "token_type": "Bearer", "expires_in": 3600, "id_token": s.IDToken(g.claims)}
	if g.refreshToken != "" {
		resp["refresh_token"] = g.refreshToken
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) serveRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("client_secret") == "" {
		http.Error(w, "invalid_client", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.revoked = append(s.revoked, r.PostForm.Get("token"))
	s.mu.Unlock()
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
