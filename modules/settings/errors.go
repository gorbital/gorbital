package settings

import (
	"errors"
	"fmt"
)

// Errors returned by [Store] methods. Check them with [errors.Is].
var (
	// ErrUnknownSetting reports a key that isn't declared in the registry.
	ErrUnknownSetting = errors.New("settings: unknown setting")

	// ErrVersionConflict reports that the setting changed after the caller
	// read it. Read it again and retry.
	ErrVersionConflict = errors.New("settings: setting changed since it was read")

	// ErrReasonRequired reports a change without a reason to a setting
	// declared with [ReasonRequired].
	ErrReasonRequired = errors.New("settings: a reason is required to change this setting")

	// ErrActorRequired reports a change without an authenticated actor in
	// the context.
	ErrActorRequired = errors.New("settings: changes require an authenticated actor")

	// ErrNotOrgOverridable reports an organisation value for a setting not
	// declared with [OrgOverridable].
	ErrNotOrgOverridable = errors.New("settings: setting can't be changed per organisation")

	// ErrInvalidOrgID reports an empty or malformed organisation ID.
	ErrInvalidOrgID = errors.New("settings: invalid organisation ID")

	// ErrInvalidValue reports a value that fails decoding or validation.
	// The error is an [*InvalidValueError].
	ErrInvalidValue = errors.New("settings: invalid value")
)

// InvalidValueError describes why a value was rejected. Reason never
// contains the rejected value.
type InvalidValueError struct {
	Key    string
	Reason string
}

func (e *InvalidValueError) Error() string {
	return fmt.Sprintf("settings: invalid value for %s: %s", e.Key, e.Reason)
}

// Unwrap returns [ErrInvalidValue].
func (e *InvalidValueError) Unwrap() error { return ErrInvalidValue }
