// Package widget makes widgets.
//
// # Usage
//
// Make one with [New]:
//
//	w := widget.New("a")
//	w.Spin()
//
// Stability: stable.
package widget

import (
	"context"
	"errors"
	"io"
)

// Kind is a widget kind.
type Kind string

// Widget kinds.
const (
	KindSmall Kind = "small" // a small widget
	KindLarge Kind = "large"
)

// MaxSize is the largest widget size.
const MaxSize = 10

// ErrBroken reports a broken widget. Check it with [errors.Is].
var ErrBroken = errors.New("widget: broken")

// Widget is a thing that spins.
type Widget struct {
	// Name names the widget.
	Name string
	Kind Kind
	io.Reader
	size int
}

// New returns a widget named name. See [Widget.Spin] and [gorbital.dev/modules/gadget.Make].
func New(name string) *Widget { return &Widget{Name: name} }

// Spin spins the widget:
//   - once
//   - quickly
func (w *Widget) Spin() int { return w.size }

// Stop stops the widget.
func (w Widget) Stop(ctx context.Context) error { return nil }

// Pair holds two values.
type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

// Swap swaps nothing.
func (p *Pair[K, V]) Swap() {}

// Spinner spins.
type Spinner interface {
	Spin() int
	stop()
}

// Helper is a plain function.
func Helper(a, b int, rest ...string) (int, error) { return a + b, nil }
