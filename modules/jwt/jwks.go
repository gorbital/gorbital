package jwt

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// Key cache bounds.
const (
	fetchTimeout       = 10 * time.Second
	minRefetchInterval = 30 * time.Second
	minCacheAge        = 5 * time.Minute
	defaultCacheAge    = time.Hour
	maxCacheAge        = 24 * time.Hour
	// maxStale is how long after its cache age a key set is still used
	// while the provider can't be reached.
	maxStale      = 24 * time.Hour
	maxJWKSBytes  = 1 << 20
	maxKeys       = 100
	minRSAKeyBits = 2048
)

// keySet caches a provider's signing keys. Lookups read the current set
// without locking; a refresh runs in its own goroutine, shared by every
// request waiting for it, so one request's cancellation doesn't abort it.
type keySet struct {
	url    string
	client *http.Client
	now    func() time.Time
	logger atomic.Pointer[slog.Logger]

	current atomic.Pointer[keys]

	mu          sync.Mutex
	fetching    chan struct{} // closed when the running fetch ends
	lastAttempt time.Time
	lastErr     error
}

type keys struct {
	byID     map[string][]jose.JSONWebKey
	all      []jose.JSONWebKey
	expires  time.Time // refresh after
	unusable time.Time // stop using after, while refreshes fail
}

func newKeySet(url string, client *http.Client, now func() time.Time) *keySet {
	s := &keySet{url: url, client: client, now: now}
	s.logger.Store(slog.New(slog.DiscardHandler))
	return s
}

func (s *keySet) setLogger(l *slog.Logger) { s.logger.Store(l) }

// load fetches the keys once, synchronously, when the authenticator is
// created.
func (s *keySet) load(ctx context.Context) error {
	s.lastAttempt = s.now()
	k, err := s.fetch(ctx)
	if err != nil {
		return err
	}
	s.current.Store(k)
	return nil
}

// lookup returns the key for a token's kid and alg.
func (s *keySet) lookup(ctx context.Context, kid string, alg jose.SignatureAlgorithm) (jose.JSONWebKey, error) {
	now := s.now()
	k := s.current.Load()
	if k != nil && now.Before(k.expires) {
		if key, ok := k.find(kid, alg); ok {
			return key, nil
		}
	}
	// Expired, or a key the cache doesn't have: refresh, rate limited.
	fresh, err := s.refresh(ctx, now)
	if fresh != nil && now.Before(fresh.unusable) {
		if key, ok := fresh.find(kid, alg); ok {
			return key, nil
		}
		if err == nil {
			return jose.JSONWebKey{}, fmt.Errorf("%w: no key %q for %s", ErrInvalidToken, kid, alg)
		}
	}
	if err == nil {
		err = errors.New("no usable keys")
	}
	return jose.JSONWebKey{}, fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
}

// refresh fetches the keys unless a fetch ran less than
// minRefetchInterval ago, waiting for a running fetch, and returns the
// current keys and the last fetch's error.
func (s *keySet) refresh(ctx context.Context, now time.Time) (*keys, error) {
	s.mu.Lock()
	ch := s.fetching
	if ch == nil {
		if now.Sub(s.lastAttempt) < minRefetchInterval && !s.lastAttempt.After(now) {
			err := s.lastErr
			s.mu.Unlock()
			return s.current.Load(), err
		}
		ch = make(chan struct{})
		s.fetching, s.lastAttempt = ch, now
		// Detached from the request: other requests wait for the same fetch.
		go s.fetchInBackground(context.WithoutCancel(ctx), ch)
	}
	s.mu.Unlock()

	select {
	case <-ch:
	case <-ctx.Done():
		return s.current.Load(), ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current.Load(), s.lastErr
}

func (s *keySet) fetchInBackground(ctx context.Context, done chan struct{}) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	k, err := s.fetch(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.lastErr = err
		s.logger.Load().WarnContext(ctx, "fetch JWKS", "url", s.url, "err", err.Error())
	} else {
		s.lastErr = nil
		s.current.Store(k)
	}
	s.fetching = nil
	close(done)
}

// fetch downloads and parses the key set.
func (s *keySet) fetch(ctx context.Context) (*keys, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, fmt.Errorf("jwt: fetch JWKS: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jwt: fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwt: fetch JWKS: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBytes+1))
	if err != nil {
		return nil, fmt.Errorf("jwt: fetch JWKS: %w", err)
	}
	if len(body) > maxJWKSBytes {
		return nil, fmt.Errorf("jwt: fetch JWKS: larger than %d bytes", maxJWKSBytes)
	}
	k, err := parseKeys(body)
	if err != nil {
		return nil, err
	}
	now := s.now()
	k.expires = now.Add(cacheAge(resp.Header.Get("Cache-Control")))
	k.unusable = k.expires.Add(maxStale)
	return k, nil
}

// parseKeys reads a JWKS document, keeping public signing keys of supported
// types and skipping the rest, so one unusual key doesn't hide the others.
func parseKeys(body []byte) (*keys, error) {
	var doc struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("jwt: JWKS is not a key set: %w", err)
	}
	k := &keys{byID: map[string][]jose.JSONWebKey{}}
	for _, raw := range doc.Keys {
		var key jose.JSONWebKey
		if json.Unmarshal(raw, &key) != nil || !usable(key) {
			continue
		}
		if len(k.all) == maxKeys {
			break
		}
		k.all = append(k.all, key)
		k.byID[key.KeyID] = append(k.byID[key.KeyID], key)
	}
	if len(k.all) == 0 {
		return nil, errors.New("jwt: JWKS has no usable signing keys")
	}
	return k, nil
}

func usable(key jose.JSONWebKey) bool {
	if !key.Valid() || !key.IsPublic() || (key.Use != "" && key.Use != "sig") {
		return false
	}
	switch pub := key.Key.(type) {
	case *rsa.PublicKey:
		return pub.N.BitLen() >= minRSAKeyBits
	case *ecdsa.PublicKey, ed25519.PublicKey:
		return true
	}
	return false
}

// find returns the key a token with kid and alg is verified with. Without
// a kid, the set must have exactly one key for alg.
func (k *keys) find(kid string, alg jose.SignatureAlgorithm) (jose.JSONWebKey, bool) {
	candidates := k.all
	if kid != "" {
		candidates = k.byID[kid]
	}
	var found []jose.JSONWebKey
	for _, key := range candidates {
		if (key.Algorithm == "" || key.Algorithm == string(alg)) && fits(key.Key, alg) {
			found = append(found, key)
		}
	}
	if len(found) == 0 || kid == "" && len(found) > 1 {
		return jose.JSONWebKey{}, false
	}
	return found[0], true
}

// fits reports whether alg is an algorithm for key's type (and curve).
func fits(key any, alg jose.SignatureAlgorithm) bool {
	switch pub := key.(type) {
	case *rsa.PublicKey:
		switch alg {
		case jose.RS256, jose.RS384, jose.RS512, jose.PS256, jose.PS384, jose.PS512:
			return true
		}
	case *ecdsa.PublicKey:
		switch alg {
		case jose.ES256:
			return pub.Curve == elliptic.P256()
		case jose.ES384:
			return pub.Curve == elliptic.P384()
		case jose.ES512:
			return pub.Curve == elliptic.P521()
		}
	case ed25519.PublicKey:
		return alg == jose.EdDSA
	}
	return false
}

// cacheAge reads max-age from a Cache-Control header, bounded; no-store,
// no-cache and missing values use the bounds and the default.
func cacheAge(header string) time.Duration {
	if header == "" {
		return defaultCacheAge
	}
	for directive := range strings.SplitSeq(header, ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
		switch strings.ToLower(name) {
		case "no-store", "no-cache":
			return minCacheAge
		case "max-age":
			seconds, err := strconv.ParseInt(strings.Trim(value, `"`), 10, 64)
			if err != nil || seconds < 0 {
				return defaultCacheAge
			}
			return min(max(time.Duration(min(seconds, int64(maxCacheAge/time.Second)))*time.Second, minCacheAge), maxCacheAge)
		}
	}
	return defaultCacheAge
}
