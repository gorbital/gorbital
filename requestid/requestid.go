// Package requestid generates, validates and carries request IDs in a
// [context.Context], so HTTP middleware, logging, audit and jobs share one
// correlation value (ADR-0030).
//
// Stability: stable (ADR-0015, ADR-0054).
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// Header is the HTTP header carrying the request ID.
const Header = "X-Request-ID"

// MaxLength is the longest accepted incoming request ID.
const MaxLength = 128

// New returns a random request ID such as "req_9f86d081884c7d65".
func New() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return "req_" + hex.EncodeToString(b)
}

// Valid reports whether id is safe to accept from a client: 1 to
// [MaxLength] characters of letters, digits, '-', '_', '.' or ':'. This keeps
// untrusted IDs from injecting content into logs.
func Valid(id string) bool {
	if id == "" || len(id) > MaxLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == ':':
		default:
			return false
		}
	}
	return true
}

type contextKey struct{}

// With returns a copy of ctx carrying id.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// From returns the request ID in ctx, or "".
func From(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
