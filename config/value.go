package config

import "context"

// A Value is a setting read each time it's used, so it can change while the
// app runs. Library modules accept a Value for options documented as live;
// apps pass a runtime setting (ADR-0031) or [Static].
//
// Get must be fast and safe for concurrent use: it's called on hot paths
// such as every login attempt.
type Value[T any] interface {
	Get(ctx context.Context) T
}

// Static returns a [Value] that always returns v.
func Static[T any](v T) Value[T] { return staticValue[T]{v: v} }

type staticValue[T any] struct{ v T }

func (s staticValue[T]) Get(context.Context) T { return s.v }
