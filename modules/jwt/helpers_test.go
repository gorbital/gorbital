package jwt

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
)

const (
	testIssuer   = "https://idp.example.com/"
	testAudience = "https://api.example.com"
)

// Keys are generated once per test binary: RSA generation is slow.
var (
	keysOnce                    sync.Once
	rsaKey, rsaKey2, rsaWeakKey *rsa.PrivateKey
	ecKey, ec384Key             *ecdsa.PrivateKey
	edKey                       ed25519.PrivateKey
)

func testKeys() {
	keysOnce.Do(func() {
		var err error
		must := func(e error) {
			if e != nil {
				panic(e)
			}
		}
		rsaKey, err = rsa.GenerateKey(rand.Reader, 2048)
		must(err)
		rsaKey2, err = rsa.GenerateKey(rand.Reader, 2048)
		must(err)
		rsaWeakKey, err = rsa.GenerateKey(rand.Reader, 1024) //nolint:gosec // a weak key the authenticator must refuse
		must(err)
		ecKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		must(err)
		ec384Key, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
		must(err)
		_, edKey, err = ed25519.GenerateKey(rand.Reader)
		must(err)
	})
}

// publicJWK returns the public JWK of a private key.
func publicJWK(priv crypto.Signer, kid, alg string) jose.JSONWebKey {
	return jose.JSONWebKey{Key: priv.Public(), KeyID: kid, Algorithm: alg, Use: "sig"}
}

// idp is a test identity provider: a JWKS server whose keys, availability
// and Cache-Control can change, counting its fetches.
type idp struct {
	t   testing.TB
	srv *httptest.Server

	mu           sync.Mutex
	keys         []jose.JSONWebKey
	raw          string // served instead of keys when set
	down         bool
	cacheControl string
	fetches      atomic.Int32
}

func newIDP(t testing.TB, keys ...jose.JSONWebKey) *idp {
	t.Helper()
	testKeys()
	p := &idp{t: t, keys: keys}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.fetches.Add(1)
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if p.cacheControl != "" {
			w.Header().Set("Cache-Control", p.cacheControl)
		}
		w.Header().Set("Content-Type", "application/json")
		if p.raw != "" {
			_, _ = w.Write([]byte(p.raw))
			return
		}
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: p.keys})
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) url() string { return p.srv.URL + "/.well-known/jwks.json" }

func (p *idp) setKeys(keys ...jose.JSONWebKey) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = keys
}

func (p *idp) setDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
}

// clock is a settable test clock.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *clock { return &clock{now: time.Unix(1_789_552_800, 0)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// claims returns valid claims at now.
func claims(now time.Time) map[string]any {
	return map[string]any{
		"iss":         testIssuer,
		"sub":         "auth0|usr_1",
		"aud":         []string{testAudience, "https://idp.example.com/userinfo"},
		"exp":         now.Add(time.Hour).Unix(),
		"iat":         now.Unix(),
		"nbf":         now.Unix(),
		"permissions": []string{"books.book.read", "books.book.write"},
	}
}

// sign returns a compact JWT of c signed with key under kid and alg.
func sign(t testing.TB, key any, kid string, alg jose.SignatureAlgorithm, c map[string]any) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithType("JWT")
	if kid != "" {
		opts = opts.WithHeader(jose.HeaderKey("kid"), kid)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: key}, opts)
	if err != nil {
		t.Fatal(err)
	}
	token, err := josejwt.Signed(signer).Claims(c).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}
