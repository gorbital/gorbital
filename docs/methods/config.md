# config

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/config"
```

Package config provides configuration helpers for the composition root of a gorbital app: a [Secret](#Secret) type that never leaks into logs or output, and environment lookup with \*\_FILE support for mounted secrets.

Library modules never read the environment; only the application's internal/app package uses this package (ADR-0020).

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Variables: [`ErrBothSet`](#ErrBothSet), [`OS`](#OS)
- Types:
  - [`Secret`](#Secret): [`NewSecret`](#NewSecret), [`Secret.Format`](#Secret.Format), [`Secret.GoString`](#Secret.GoString), [`Secret.IsZero`](#Secret.IsZero), [`Secret.LogValue`](#Secret.LogValue), [`Secret.MarshalText`](#Secret.MarshalText), [`Secret.Reveal`](#Secret.Reveal), [`Secret.String`](#Secret.String), [`Secret.UnmarshalText`](#Secret.UnmarshalText)
  - [`Source`](#Source): [`Source.Get`](#Source.Get), [`Source.Secret`](#Source.Secret)
  - [`Value`](#Value): [`Static`](#Static)

## Variables

<a id="ErrBothSet"></a>

```go
var ErrBothSet = errors.New("config: both variable and _FILE variant are set")
```

ErrBothSet reports that both KEY and KEY\_FILE are set.

*Since `v0.1.0`*

<a id="OS"></a>

```go
var OS = Source{Getenv: os.Getenv, ReadFile: os.ReadFile}
```

OS reads the process environment and the filesystem.

*Since `v0.1.0`*

## Types

<a id="Secret"></a>

### type Secret

```go
type Secret struct {
	// contains filtered or unexported fields
}
```

Secret holds a sensitive value such as an API key or password. Formatting, logging and JSON/text marshaling all print "\[redacted]". Use [Secret.Reveal](#Secret.Reveal) to read the value.

The zero value is an empty secret.

*Since `v0.1.0`*

<a id="NewSecret"></a>

#### func NewSecret

```go
func NewSecret(value string) Secret
```

NewSecret wraps value.

*Since `v0.1.0`*

<a id="Secret.Format"></a>

#### func (Secret) Format

```go
func (s Secret) Format(f fmt.State, _ rune)
```

Format prints "\[redacted]" for every verb.

*Since `v0.1.0`*

<a id="Secret.GoString"></a>

#### func (Secret) GoString

```go
func (s Secret) GoString() string
```

GoString returns "\[redacted]".

*Since `v0.1.0`*

<a id="Secret.IsZero"></a>

#### func (Secret) IsZero

```go
func (s Secret) IsZero() bool
```

IsZero reports whether the secret is empty.

*Since `v0.1.0`*

<a id="Secret.LogValue"></a>

#### func (Secret) LogValue

```go
func (s Secret) LogValue() slog.Value
```

LogValue makes slog print "\[redacted]".

*Since `v0.1.0`*

<a id="Secret.MarshalText"></a>

#### func (Secret) MarshalText

```go
func (s Secret) MarshalText() ([]byte, error)
```

MarshalText returns "\[redacted]", so JSON and other encoders never expose the value.

*Since `v0.1.0`*

<a id="Secret.Reveal"></a>

#### func (Secret) Reveal

```go
func (s Secret) Reveal() string
```

Reveal returns the secret value.

*Since `v0.1.0`*

<a id="Secret.String"></a>

#### func (Secret) String

```go
func (s Secret) String() string
```

String returns "\[redacted]".

*Since `v0.1.0`*

<a id="Secret.UnmarshalText"></a>

#### func (*Secret) UnmarshalText

```go
func (s *Secret) UnmarshalText(text []byte) error
```

UnmarshalText stores text as the secret value. Environment loaders that support encoding.TextUnmarshaler use it.

*Since `v0.1.0`*

<a id="Source"></a>
<a id="Source.Getenv"></a>
<a id="Source.ReadFile"></a>

### type Source

```go
type Source struct {
	Getenv   func(key string) string
	ReadFile func(name string) ([]byte, error)
}
```

Source reads configuration values. The zero value is not usable; use [OS](#OS) or construct one with test doubles.

*Since `v0.1.0`*

<a id="Source.Get"></a>

#### func (Source) Get

```go
func (s Source) Get(key string) (string, error)
```

Get returns the value of key. If key is empty and key+"\_FILE" names a file, Get returns the file's contents with surrounding whitespace removed; this supports Docker and Kubernetes secrets mounted as files. Setting both returns [ErrBothSet](#ErrBothSet). A missing key returns "".

*Since `v0.1.0`*

<a id="Source.Secret"></a>

#### func (Source) Secret

```go
func (s Source) Secret(key string) (Secret, error)
```

Secret is like [Source.Get](#Source.Get) but wraps the value in a [Secret](#Secret).

*Since `v0.1.0`*

<a id="Value"></a>
<a id="Value.Get"></a>

### type Value

```go
type Value[T any] interface {
	Get(ctx context.Context) T
}
```

A Value is a setting read each time it's used, so it can change while the app runs. Library modules accept a Value for options documented as live; apps pass a runtime setting (ADR-0031) or [Static](#Static).

Get must be fast and safe for concurrent use: it's called on hot paths such as every login attempt.

*Since `v0.1.0`*

<a id="Static"></a>

#### func Static

```go
func Static[T any](v T) Value[T]
```

Static returns a [Value](#Value) that always returns v.

*Since `v0.1.0`*
