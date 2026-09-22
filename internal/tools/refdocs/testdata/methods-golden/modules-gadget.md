# modules/gadget

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/gadget"
```

Package gadget makes gadgets.

## Contents

- Constants: [`Loose`](#Loose), [`Tight`](#Tight)
- Functions: [`Fresh`](#Fresh), [`Make`](#Make), [`Old`](#Old), [`Undocumented`](#Undocumented)
- Types:
  - [`Gadget`](#Gadget): [`Gadget.Run`](#Gadget.Run)

## Constants

<a id="Loose"></a>
<a id="Tight"></a>

```go
const (
	Loose = 1
	// Tight is documented.
	Tight = 2
)
```

*Since `v0.1.0`: Loose; `v0.4.0 (unreleased)`: Tight*

## Functions

<a id="Fresh"></a>

### func Fresh

```go
func Fresh()
```

Fresh is new and has no example.

*Since `v0.4.0 (unreleased)`*

<a id="Make"></a>

### func Make

```go
func Make()
```

Make makes a gadget. It is new and has an example.

*Since `v0.4.0 (unreleased)`*

**Example**

```go
Make()
```

<a id="Old"></a>

### func Old

```go
func Old()
```

Old was released and has no example.

*Since `v0.1.0`*

<a id="Undocumented"></a>

### func Undocumented

```go
func Undocumented()
```

*Since `v0.4.0 (unreleased)`*

## Types

<a id="Gadget"></a>

### type Gadget

```go
type Gadget struct{}
```

Gadget is a gadget.

*Since `v0.1.0`*

**Example**

```go
_ = Gadget{}
```

<a id="Gadget.Run"></a>

#### func (Gadget) Run

```go
func (Gadget) Run()
```

*Since `v0.4.0 (unreleased)`*

**Example**

```go
Gadget{}.Run()
```
