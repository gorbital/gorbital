// Package domain holds the feature flags module's errors. It imports only
// the standard library.
package domain

import "errors"

// ErrUnauthenticated reports a request without a signed-in caller: flags
// are evaluated for someone.
var ErrUnauthenticated = errors.New("authentication is required")

// ErrForbidden reports a caller without flags.flag.read: an API key whose
// scopes leave it out.
var ErrForbidden = errors.New("missing permission")
