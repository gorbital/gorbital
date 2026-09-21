// Package usecase is the near-miss fixture's rules.
package usecase

import (
	"crypto/rand"
	"crypto/subtle"
	"log/slog"
	mathrand "math/rand/v2"
	"time"
)

// crypto/rand, in a function whose name says the value is a secret.
func NewSessionToken() string { return rand.Text() }

// math/rand for jitter, in a file that is all about tokens: the value it
// produces is a delay, and nothing here calls it a secret.
func RetryDelay() time.Duration {
	jitter := mathrand.N(500 * time.Millisecond)
	return time.Second + jitter
}

// A constant-time comparison, and the presence tests that read like
// comparisons and are not.
func SessionMatches(sentToken, storedToken string) bool {
	if sentToken == "" || storedToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(sentToken), []byte(storedToken)) == 1
}

// A comparison against a constant the app declares: the name of a sign-in
// method, not a password.
const MethodPassword = "password"

func IsPassword(method string) bool { return method == MethodPassword }

// The log says which key, not what it is.
func IssueAPIKey(log *slog.Logger, id, keyID string, apiKey string) {
	log.Info("issued an API key", "id", id, "key_id", keyID)
	_ = apiKey
}
