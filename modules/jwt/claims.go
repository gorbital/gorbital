package jwt

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Claims are a verified token's claims: the registered ones as fields, and
// any other through [Claims.String], [Claims.Strings] and [Claims.Decode].
type Claims struct {
	// Issuer is "iss", equal to [Config.Issuer].
	Issuer string
	// Subject is "sub", the caller's ID at the provider. Never empty.
	Subject string
	// Audience is "aud".
	Audience []string
	// ExpiresAt is "exp"; NotBefore "nbf" and IssuedAt "iat", or zero when
	// the token has none.
	ExpiresAt, NotBefore, IssuedAt time.Time
	// ID is "jti", or empty.
	ID string

	raw map[string]json.RawMessage
}

// String returns a string claim, or "" when the claim is missing or isn't
// a string.
func (c Claims) String(name string) string {
	var s string
	if json.Unmarshal(c.raw[name], &s) != nil {
		return ""
	}
	return s
}

// Strings returns a claim holding a list of strings, such as
// "permissions", or one space-separated string, such as "scope". It returns
// nil when the claim is missing or is neither.
func (c Claims) Strings(name string) []string {
	raw, ok := c.raw[name]
	if !ok {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.Fields(s)
	}
	return nil
}

// ErrNoClaim reports a claim the token doesn't carry.
var ErrNoClaim = errors.New("jwt: no such claim")

// Decode unmarshals a claim's JSON into v, such as a provider's nested
// metadata object. It returns an error wrapping [ErrNoClaim] when the claim
// is missing, or the JSON error when it doesn't fit v.
func (c Claims) Decode(name string, v any) error {
	raw, ok := c.raw[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNoClaim, name)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("jwt: claim %q: %w", name, err)
	}
	return nil
}
