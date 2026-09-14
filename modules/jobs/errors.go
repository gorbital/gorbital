package jobs

import (
	"errors"
	"fmt"
)

// Errors returned by [Manager] methods. Check them with [errors.Is].
var (
	// ErrUnknownDefinition reports a name that no definition declares.
	ErrUnknownDefinition = errors.New("jobs: unknown job definition")

	// ErrVersionConflict reports that the definition changed after the
	// caller read it. Read it again and retry.
	ErrVersionConflict = errors.New("jobs: job definition changed since it was read")

	// ErrReasonRequired reports disabling or rescheduling a job without a
	// reason.
	ErrReasonRequired = errors.New("jobs: a reason is required to disable or reschedule a job")

	// ErrActorRequired reports a change without an authenticated actor in
	// the context.
	ErrActorRequired = errors.New("jobs: changes require an authenticated actor")

	// ErrDefinitionDisabled reports running a disabled job on demand.
	ErrDefinitionDisabled = errors.New("jobs: job definition is disabled")

	// ErrJobNotFound reports a job ID that doesn't exist, for example
	// because retention removed it.
	ErrJobNotFound = errors.New("jobs: job not found")

	// ErrUnknownQueue reports a queue no worker runs.
	ErrUnknownQueue = errors.New("jobs: queue is not active")

	// ErrInvalidConfig reports configuration outside the allowed bounds.
	// The error is an [*InvalidConfigError].
	ErrInvalidConfig = errors.New("jobs: invalid job configuration")

	// ErrInvalidCursor reports a malformed pagination cursor.
	ErrInvalidCursor = errors.New("jobs: invalid cursor")
)

// InvalidConfigError describes why a configuration change was rejected.
type InvalidConfigError struct {
	Name   string
	Reason string
}

func (e *InvalidConfigError) Error() string {
	return fmt.Sprintf("jobs: invalid configuration for %s: %s", e.Name, e.Reason)
}

// Unwrap returns [ErrInvalidConfig].
func (e *InvalidConfigError) Unwrap() error { return ErrInvalidConfig }
