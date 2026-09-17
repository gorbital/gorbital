package flags

import (
	"errors"
	"fmt"
)

// Errors returned by [Store] methods. Check them with [errors.Is].
var (
	// ErrUnknownFlag reports a key that isn't declared in the registry.
	ErrUnknownFlag = errors.New("flags: unknown flag")

	// ErrVersionConflict reports that the flag changed after the caller
	// read it. Read it again and retry.
	ErrVersionConflict = errors.New("flags: flag changed since it was read")

	// ErrReasonRequired reports a change without a reason. Every flag change
	// needs one: flags change what users get in production.
	ErrReasonRequired = errors.New("flags: a reason is required to change a flag")

	// ErrActorRequired reports a change without an authenticated actor in
	// the context.
	ErrActorRequired = errors.New("flags: changes require an authenticated actor")

	// ErrInvalidState reports a state outside the bounds. The error is an
	// [*InvalidStateError].
	ErrInvalidState = errors.New("flags: invalid state")
)

// InvalidStateError describes why a state was rejected. Reason names the
// field and never contains an ID.
type InvalidStateError struct {
	Key    string
	Reason string
}

// Error names the flag key and the reason the state was rejected.
func (e *InvalidStateError) Error() string {
	return fmt.Sprintf("flags: invalid state for %s: %s", e.Key, e.Reason)
}

// Unwrap returns [ErrInvalidState].
func (e *InvalidStateError) Unwrap() error { return ErrInvalidState }
