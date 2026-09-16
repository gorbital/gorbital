// Package domain holds the feature flags module's errors. It imports only
// the standard library.
package domain

import "errors"

// ErrUnauthenticated reports a request without a signed-in caller: flags
// are evaluated for someone.
var ErrUnauthenticated = errors.New("authentication is required")
