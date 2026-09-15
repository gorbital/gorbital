package actor

import (
	"context"
	"errors"
	"slices"
)

// Errors returned by [Require]. Check them with [errors.Is].
var (
	// ErrUnauthenticated reports an operation without an actor, or with
	// the anonymous actor.
	ErrUnauthenticated = errors.New("actor: authentication required")
	// ErrForbidden reports an actor without the permission.
	ErrForbidden = errors.New("actor: permission denied")
	// ErrStepUpRequired reports an actor whose roles grant the permission
	// only after a stronger sign-in, such as two-factor authentication.
	ErrStepUpRequired = errors.New("actor: stronger sign-in required")
)

// Require checks that the actor in ctx holds permission. It returns
// [ErrUnauthenticated] without an actor, [ErrStepUpRequired] when the
// permission is in the actor's StepUp, and [ErrForbidden] otherwise.
func Require(ctx context.Context, permission string) error {
	a, ok := From(ctx)
	switch {
	case !ok || a.Kind == KindAnonymous:
		return ErrUnauthenticated
	case a.Can(permission):
		return nil
	case slices.Contains(a.StepUp, permission):
		return ErrStepUpRequired
	default:
		return ErrForbidden
	}
}
