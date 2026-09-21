// Package usecase is the flawed fixture's rules.
package usecase

import (
	"log/slog"
	"math/rand/v2"
)

const tokenAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// weak-random-secret: the function's name says what the value is for.
func NewSessionToken() string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = tokenAlphabet[rand.IntN(len(tokenAlphabet))]
	}
	return string(b)
}

// secret-compared-directly: both sides are values, so the comparison's
// duration says how much of the token was right.
func SessionMatches(sentToken, storedToken string) bool {
	return sentToken == storedToken
}

// secret-in-log: the key reaches wherever the logs go.
func IssueAPIKey(log *slog.Logger, id, apiKey string) {
	log.Info("issued an API key", "id", id, "key", apiKey)
}
