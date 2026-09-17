# requestid

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/requestid"
```

Package requestid generates, validates and carries request IDs in a [context.Context](https://pkg.go.dev/context#Context), so HTTP middleware, logging, audit and jobs share one correlation value (ADR-0030).

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`Header`](#Header), [`MaxLength`](#MaxLength)
- Functions: [`From`](#From), [`New`](#New), [`Valid`](#Valid), [`With`](#With)

## Constants

<a id="Header"></a>

```go
const Header = "X-Request-ID"
```

Header is the HTTP header carrying the request ID.

*Since `v0.1.0`*

<a id="MaxLength"></a>

```go
const MaxLength = 128
```

MaxLength is the longest accepted incoming request ID.

*Since `v0.1.0`*

## Functions

<a id="From"></a>

### func From

```go
func From(ctx context.Context) string
```

From returns the request ID in ctx, or "".

*Since `v0.1.0`*

<a id="New"></a>

### func New

```go
func New() string
```

New returns a random request ID such as "req\_9f86d081884c7d65".

*Since `v0.1.0`*

<a id="Valid"></a>

### func Valid

```go
func Valid(id string) bool
```

Valid reports whether id is safe to accept from a client: 1 to [MaxLength](#MaxLength) characters of letters, digits, '-', '\_', '.' or ':'. This keeps untrusted IDs from injecting content into logs.

*Since `v0.1.0`*

<a id="With"></a>

### func With

```go
func With(ctx context.Context, id string) context.Context
```

With returns a copy of ctx carrying id.

*Since `v0.1.0`*
