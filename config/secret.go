// Package config provides configuration helpers for the composition root of
// an gorbital app: a [Secret] type that never leaks into logs or output, and
// environment lookup with *_FILE support for mounted secrets.
//
// Library modules never read the environment; only the application's
// internal/app package uses this package (ADR-0020).
//
// Stability: pre-1.0 (ADR-0015).
package config

import (
	"fmt"
	"log/slog"
)

const redacted = "[redacted]"

// Secret holds a sensitive value such as an API key or password. Formatting,
// logging and JSON/text marshaling all print "[redacted]". Use
// [Secret.Reveal] to read the value.
//
// The zero value is an empty secret.
type Secret struct {
	value string
}

// NewSecret wraps value.
func NewSecret(value string) Secret { return Secret{value: value} }

// Reveal returns the secret value.
func (s Secret) Reveal() string { return s.value }

// IsZero reports whether the secret is empty.
func (s Secret) IsZero() bool { return s.value == "" }

// String returns "[redacted]".
func (s Secret) String() string { return redacted }

// GoString returns "[redacted]".
func (s Secret) GoString() string { return redacted }

// Format prints "[redacted]" for every verb.
func (s Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redacted)) }

// LogValue makes slog print "[redacted]".
func (s Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalText returns "[redacted]", so JSON and other encoders never expose
// the value.
func (s Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// UnmarshalText stores text as the secret value. Environment loaders that
// support encoding.TextUnmarshaler use it.
func (s *Secret) UnmarshalText(text []byte) error {
	s.value = string(text)
	return nil
}
