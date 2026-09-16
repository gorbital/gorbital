// Package actor records who is performing an operation, in a
// [context.Context]. It knows nothing about how the actor was authenticated;
// authentication middleware sets the actor, and audit, jobs and use cases
// read it (ADR-0030).
//
// Stability: stable (ADR-0015, ADR-0054).
package actor

import (
	"context"
	"slices"
)

// Kind classifies an actor.
type Kind string

// Actor kinds.
const (
	KindUser      Kind = "user"
	KindService   Kind = "service"
	KindSystem    Kind = "system"
	KindAnonymous Kind = "anonymous"
)

// An Actor is the identity performing an operation.
type Actor struct {
	Kind Kind
	// ID is the stable identifier (for example a user ID). Empty for anonymous actors.
	ID string
	// Label is a human-readable name captured at the time of the action.
	Label string
	// OrgID is the organisation the actor is acting in, if any.
	OrgID string
	// Permissions are the permissions granted for this operation.
	Permissions []string
	// StepUp are permissions the actor's roles grant but this operation
	// doesn't get until the actor signs in more strongly, for example with
	// two-factor authentication. They are never granted by Can.
	StepUp []string
}

// Anonymous is the actor for unauthenticated operations.
var Anonymous = Actor{Kind: KindAnonymous}

// System returns an actor for work the application performs itself, such
// as a scheduled job.
func System(name string) Actor {
	return Actor{Kind: KindSystem, ID: name, Label: name}
}

// Can reports whether the actor holds permission.
func (a Actor) Can(permission string) bool {
	return slices.Contains(a.Permissions, permission)
}

type contextKey struct{}

// With returns a copy of ctx carrying a. The permission slices are copied.
func With(ctx context.Context, a Actor) context.Context {
	a.Permissions, a.StepUp = slices.Clone(a.Permissions), slices.Clone(a.StepUp)
	return context.WithValue(ctx, contextKey{}, a)
}

// From returns the actor stored in ctx and whether one was set.
func From(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(contextKey{}).(Actor)
	return a, ok
}

// FromOrAnonymous returns the actor stored in ctx, or [Anonymous].
func FromOrAnonymous(ctx context.Context) Actor {
	if a, ok := From(ctx); ok {
		return a
	}
	return Anonymous
}
