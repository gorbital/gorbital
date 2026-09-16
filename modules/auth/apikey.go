package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// API keys (ADR-0058) look like gbk_<lookup ID>_<secret>: the prefix lets
// middleware, secret scanners and people recognise them; the lookup ID (128
// random bits, 26 characters) finds the stored key and may be logged; the
// secret (256 random bits, 52 characters) is never stored or logged. Both
// parts use lowercase base32, so a key has no characters that need escaping
// and selects with a double click.
const (
	// APIKeyPrefix starts every API key.
	APIKeyPrefix = "gbk_"
	// APIKeyLength is the length of every API key.
	APIKeyLength = len(APIKeyPrefix) + apiKeyLookupLength + 1 + apiKeySecretLength

	apiKeyLookupLength = 26 // 16 bytes
	apiKeySecretLength = 52 // 32 bytes
)

// Defaults and limits for API keys.
const (
	// DefaultAPIKeyMaxTTL is the longest lifetime a new API key may have by
	// default; apps read it from a runtime setting clamped to APIKeyTTLLimits.
	DefaultAPIKeyMaxTTL = 90 * 24 * time.Hour
	// APIKeyTouchInterval is the shortest time between two last-used updates
	// of an API key, so busy clients don't write on every request.
	APIKeyTouchInterval = time.Minute
	// APIKeyRetention is how long expired and revoked API keys stay listed
	// before cleanup removes them.
	APIKeyRetention = 30 * 24 * time.Hour
)

// APIKeyTTLLimits bounds the longest lifetime of a new API key: however an
// operator sets it, a key lives at least an hour and at most a year.
var APIKeyTTLLimits = Limits{Min: time.Hour, Max: 365 * 24 * time.Hour}

// NewAPIKey returns a new API key, its lookup ID and the hash to store.
// Show the key once and store only the lookup ID and hash.
func NewAPIKey() (key, lookupID string, hash []byte) {
	lookup, secret := make([]byte, 16), make([]byte, 32)
	_, _ = rand.Read(lookup) // never fails (crypto/rand)
	_, _ = rand.Read(secret)
	lookupID = idEncoding.EncodeToString(lookup)
	key = APIKeyPrefix + lookupID + "_" + idEncoding.EncodeToString(secret)
	return key, lookupID, HashAPIKey(key)
}

// IsAPIKey reports whether token has the API key prefix. A token with the
// prefix is never a session token: [Middleware] authenticates it only as an
// API key, and [NewToken] never returns one.
func IsAPIKey(token string) bool {
	return strings.HasPrefix(token, APIKeyPrefix)
}

// ParseAPIKey returns the lookup ID of a well-formed API key, or
// [ErrUnauthenticated]. It checks only the format: look the key up by its
// lookup ID, then compare with [APIKeyMatches].
func ParseAPIKey(key string) (lookupID string, err error) {
	if len(key) != APIKeyLength || !IsAPIKey(key) {
		return "", ErrUnauthenticated
	}
	rest := key[len(APIKeyPrefix):]
	lookupID, secret := rest[:apiKeyLookupLength], rest[apiKeyLookupLength+1:]
	if rest[apiKeyLookupLength] != '_' || !isBase32Lower(lookupID) || !isBase32Lower(secret) {
		return "", ErrUnauthenticated
	}
	return lookupID, nil
}

func isBase32Lower(s string) bool {
	for i := range len(s) {
		if c := s[i]; (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

// HashAPIKey returns the SHA-256 of an API key, for storing. The secret has
// 256 random bits, so a fast hash is enough: nobody can guess it from the
// hash.
func HashAPIKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

// APIKeyMatches reports, in constant time, whether key is the one hashed as
// hash. A nil or short hash never matches, but takes the same time.
func APIKeyMatches(key string, hash []byte) bool {
	want := make([]byte, sha256.Size)
	copy(want, hash)
	return subtle.ConstantTimeCompare(HashAPIKey(key), want) == 1 && len(hash) == sha256.Size
}

// An APIKeyAuthenticator resolves an API key, returning
// [ErrUnauthenticated] for an unknown, malformed, expired or revoked key,
// or one whose owner is deleted or disabled, and a [*RateLimitError] for a
// client that failed too often. The app's auth use cases implement it.
type APIKeyAuthenticator interface {
	AuthenticateAPIKey(ctx context.Context, key string) (Principal, error)
}

// ErrRateLimited reports a client that failed to authenticate too often.
// The error is a [*RateLimitError].
var ErrRateLimited = errors.New("auth: too many failed authentications")

// RateLimitError reports how long a client waits before authenticating
// again.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("auth: too many failed authentications; retry in %s", e.RetryAfter.Round(time.Second))
}

// Unwrap returns [ErrRateLimited].
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// APIKey reports whether an API key, rather than a session, authenticated
// the principal.
func (p Principal) APIKey() bool { return p.APIKeyID != "" }

// Restrict returns the permissions a principal may use from granted and
// stepUp, as computed from its roles. A session keeps both. An API key never
// gets step-up permissions, since it can't sign in with a second factor, and
// keeps only the granted permissions in its Scopes when it has any
// (ADR-0058). Every path that turns roles into an API key's permissions
// passes through it.
func (p Principal) Restrict(granted, stepUp []string) (permissions, stepUpPermissions []string) {
	if !p.APIKey() {
		return granted, stepUp
	}
	if len(p.Scopes) == 0 {
		return slices.Clone(granted), nil
	}
	return slices.DeleteFunc(slices.Clone(granted), func(perm string) bool { return !slices.Contains(p.Scopes, perm) }), nil
}
