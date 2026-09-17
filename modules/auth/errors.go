package auth

import "errors"

// Errors returned by the helpers. Check them with [errors.Is].
var (
	// ErrInvalidEmail reports an email address that isn't valid.
	ErrInvalidEmail = errors.New("auth: invalid email address")

	// ErrWeakPassword reports a password that fails the password policy. The
	// error is a [*PasswordError].
	ErrWeakPassword = errors.New("auth: password does not meet the policy")

	// ErrUnauthenticated reports a missing, invalid, expired or revoked
	// session.
	ErrUnauthenticated = errors.New("auth: authentication required")
)

// PasswordError explains why a password was rejected. Reason never
// contains the password.
type PasswordError struct {
	Reason string
}

// Error returns "auth: password " followed by Reason.
func (e *PasswordError) Error() string { return "auth: password " + e.Reason }

// Unwrap returns [ErrWeakPassword].
func (e *PasswordError) Unwrap() error { return ErrWeakPassword }
