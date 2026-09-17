# widget

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/widget"
```

Package widget makes widgets.

## Usage

Make one with [New](#New):

```go
w := widget.New("a")
w.Spin()
```

Stability: stable.

Use widgets for [small things](../guides/widgets.md).

## Contents

- Constants: [`MaxSize`](#MaxSize)
- Variables: [`ErrBroken`](#ErrBroken)
- Functions: [`Helper`](#Helper)
- Types:
  - [`Kind`](#Kind): [`KindSmall`](#KindSmall), [`KindLarge`](#KindLarge)
  - [`Pair`](#Pair): [`Pair.Swap`](#Pair.Swap)
  - [`Spinner`](#Spinner)
  - [`Widget`](#Widget): [`New`](#New), [`Widget.Spin`](#Widget.Spin), [`Widget.Stop`](#Widget.Stop)

## Constants

<a id="MaxSize"></a>

```go
const MaxSize = 10
```

MaxSize is the largest widget size.

*Since `v0.1.0`*

## Variables

<a id="ErrBroken"></a>

```go
var ErrBroken = errors.New("widget: broken")
```

ErrBroken reports a broken widget. Check it with [errors.Is](https://pkg.go.dev/errors#Is).

*Since `v0.1.0`*

## Functions

<a id="Helper"></a>

### func Helper

```go
func Helper(a, b int, rest ...string) (int, error)
```

Helper is a plain function.

*Since `v0.1.0`*

## Types

<a id="Kind"></a>

### type Kind

```go
type Kind string
```

Kind is a widget kind.

*Since `v0.1.0`*

<a id="KindSmall"></a>
<a id="KindLarge"></a>

```go
const (
	KindSmall Kind = "small" // a small widget
	KindLarge Kind = "large"
)
```

Widget kinds.

*Since `v0.1.0`*

<a id="Pair"></a>
<a id="Pair.Key"></a>
<a id="Pair.Value"></a>

### type Pair

```go
type Pair[K comparable, V any] struct {
	Key   K
	Value V
}
```

Pair holds two values.

*Since `v0.1.0`*

<a id="Pair.Swap"></a>

#### func (*Pair[K, V]) Swap

```go
func (p *Pair[K, V]) Swap()
```

Swap swaps nothing.

*Since `v0.1.0`*

<a id="Spinner"></a>
<a id="Spinner.Spin"></a>

### type Spinner

```go
type Spinner interface {
	Spin() int
	// contains filtered or unexported methods
}
```

Spinner spins.

*Since `v0.1.0`*

<a id="Widget"></a>
<a id="Widget.Name"></a>
<a id="Widget.Kind"></a>
<a id="Widget.Reader"></a>

### type Widget

```go
type Widget struct {
	// Name names the widget.
	Name string
	Kind Kind
	io.Reader
	// contains filtered or unexported fields
}
```

Widget is a thing that spins.

*Since `v0.1.0`*

<a id="New"></a>

#### func New

```go
func New(name string) *Widget
```

New returns a widget named name. See [Widget.Spin](#Widget.Spin) and [gorbital.dev/modules/gadget.Make](modules-gadget.md#Make).

*Since `v0.1.0`*

**Example**

```go
w := widget.New("a")
fmt.Println(w.Name)
```

Output:

```text
a
```

<a id="Widget.Spin"></a>

#### func (*Widget) Spin

```go
func (w *Widget) Spin() int
```

Spin spins the widget:

  - once
  - quickly

*Since `v0.1.0`*

**Example (twice)**

A widget spins.

```go
w := widget.New("b")
w.Spin()
w.Spin()
```

<a id="Widget.Stop"></a>

#### func (Widget) Stop

```go
func (w Widget) Stop(ctx context.Context) error
```

Stop stops the widget.

*Since `v0.1.0`*
