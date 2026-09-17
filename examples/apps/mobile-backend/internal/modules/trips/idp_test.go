package trips_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
)

// The issuer and audience of the tests, in the reserved .test domain: an
// app's real values come from IDP_ISSUER and IDP_AUDIENCE.
const (
	testIssuer   = "https://idp.example.test/"
	testAudience = "https://api.mobile-backend.test"
)

// testRSAKeys are generated once per test binary: RSA generation is slow.
// The first key is the provider's, the second the one it rotates to, and
// the third belongs to nobody the app trusts.
var testRSAKeys = sync.OnceValue(func() []*rsa.PrivateKey {
	keys := make([]*rsa.PrivateKey, 3)
	for i := range keys {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		keys[i] = key
	}
	return keys
})

// docs:start test-idp

// idp is a local stand-in for the identity provider: an HTTP server
// publishing a key set, and the private key it signs tokens with. A real
// provider publishes the same JSON at its JWKS URL, so the app under test
// is configured exactly as it is in production, with IDP_JWKS_URL pointing
// at this server.
type idp struct {
	srv *httptest.Server
	// clock is the time the provider and the authenticator share, so a test
	// can move past the authenticator's 30-second refetch interval without
	// sleeping.
	clock *testClock

	mu   sync.Mutex
	key  *rsa.PrivateKey   // signs new tokens
	kid  string            // its key ID
	keys []jose.JSONWebKey // what the JWKS says
	down bool              // answers 502, as an unreachable provider
}

func newIDP(t *testing.T) *idp {
	t.Helper()
	p := &idp{clock: &testClock{now: time.Now()}, key: testRSAKeys()[0], kid: "key-1"}
	p.keys = []jose.JSONWebKey{publicKey(p.key, p.kid)}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.mu.Lock()
		down, keys := p.down, p.keys
		p.mu.Unlock()
		if down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: keys})
	}))
	t.Cleanup(p.srv.Close)
	return p
}

// url is what IDP_JWKS_URL holds: http on a loopback address, which the
// authenticator allows for a provider running locally.
func (p *idp) url() string { return p.srv.URL + "/.well-known/jwks.json" }

// token returns an access token for the traveller sub, holding permissions
// in the claim the authenticator reads, signed with the provider's current
// key.
func (p *idp) token(sub string, permissions ...string) string {
	key, kid := p.signingKey()
	return p.sign(key, kid, p.claims(sub, permissions))
}

// signingKey is the key the provider signs with now.
func (p *idp) signingKey() (*rsa.PrivateKey, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.key, p.kid
}

// claims are the claims of a valid token now: the registered ones the
// authenticator checks, and the permissions claim the guards read.
func (p *idp) claims(sub string, permissions []string) map[string]any {
	now := p.clock.Now()
	return map[string]any{
		"iss":         testIssuer,
		"aud":         testAudience,
		"sub":         sub,
		"iat":         now.Unix(),
		"exp":         now.Add(time.Hour).Unix(),
		"permissions": permissions,
	}
}

// sign returns claims as a compact RS256 JWT signed with key under kid.
func (p *idp) sign(key *rsa.PrivateKey, kid string, claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	if err != nil {
		panic(err)
	}
	token, err := josejwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		panic(err)
	}
	return token
}

// docs:end test-idp

// rotate publishes key under kid and signs new tokens with it, as a
// provider does when it rotates its signing key.
func (p *idp) rotate(key *rsa.PrivateKey, kid string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.key, p.kid = key, kid
	p.keys = []jose.JSONWebKey{publicKey(key, kid)}
}

// setDown makes the JWKS server answer 502, as a provider nobody can reach.
func (p *idp) setDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
}

func publicKey(key *rsa.PrivateKey, kid string) jose.JSONWebKey {
	return jose.JSONWebKey{Key: key.Public(), KeyID: kid, Algorithm: string(jose.RS256), Use: "sig"}
}

// testClock is a clock the test moves, shared by the provider and the
// authenticator (jwt.WithClock).
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
