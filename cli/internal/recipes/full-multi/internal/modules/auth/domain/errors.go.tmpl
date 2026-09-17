package domain

import (
	"errors"
	"fmt"
	"time"
)

// Errors returned by the auth use cases. Errors about email addresses,
// passwords and sessions come from the gorbital auth module
// (auth.ErrInvalidEmail, auth.ErrWeakPassword, auth.ErrUnauthenticated).
var (
	// ErrInvalidCredentials reports a wrong email or password, without
	// saying which.
	ErrInvalidCredentials = errors.New("invalid email or password")

	// ErrEmailNotVerified reports a correct password for an account whose
	// email isn't verified yet.
	ErrEmailNotVerified = errors.New("email address is not verified")

	// ErrInvalidCode reports a wrong, expired, used or exhausted code, or an
	// address without a pending code.
	ErrInvalidCode = errors.New("invalid or expired code")

	// ErrTooManyAttempts reports rate limiting. The error is a
	// *RateLimitError.
	ErrTooManyAttempts = errors.New("too many attempts")

	// ErrSessionNotFound reports a session that doesn't exist or doesn't
	// belong to the user.
	ErrSessionNotFound = errors.New("session not found")

	// ErrUserNotFound reports an unknown or deleted account.
	ErrUserNotFound = errors.New("user not found")

	// ErrEmailTaken reports an address that already has an account.
	ErrEmailTaken = errors.New("email address already has an account")

	// ErrUnknownRole reports a role that isn't in the permission catalog.
	ErrUnknownRole = errors.New("unknown role")

	// ErrActorRequired reports an operator action without an authenticated
	// actor.
	ErrActorRequired = errors.New("an authenticated actor is required")
	// ErrAccountBanned reports a sign-in to an account an operator banned
	// (ADR-0070).
	ErrAccountBanned = errors.New("the account is banned")
	// ErrImpersonationOff reports an impersonation outside development:
	// the app runs without the dev console.
	ErrImpersonationOff = errors.New("impersonation is available only in development")
	// ErrInvalidCursor reports a users cursor that isn't one this app made.
	ErrInvalidCursor = errors.New("invalid cursor")
)

// RateLimitError reports how long to wait before trying again.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("too many attempts; retry in %s", e.RetryAfter.Round(time.Second))
}

// Unwrap returns ErrTooManyAttempts.
func (e *RateLimitError) Unwrap() error { return ErrTooManyAttempts }
