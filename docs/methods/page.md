# page

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/page"
```

Package page provides cursor pagination and sorting for list endpoints: bounded limits, opaque cursors and allowlisted sort fields.

Embed [Params](#Params) in a Huma input struct, or call [Parse](#Parse) with URL query values, then convert to a validated [Request](#Request) against the endpoint's [Options](#Options).

Cursors are opaque but not signed. They must only encode positions (for example the last item's sort key and ID); repositories still apply authorisation and tenant filters to every query.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`DefaultLimit`](#DefaultLimit), [`MaxLimit`](#MaxLimit), [`MaxCursorLength`](#MaxCursorLength)
- Variables: [`ErrInvalidLimit`](#ErrInvalidLimit), [`ErrInvalidCursor`](#ErrInvalidCursor), [`ErrInvalidSort`](#ErrInvalidSort)
- Functions: [`DecodeCursor`](#DecodeCursor), [`EncodeCursor`](#EncodeCursor)
- Types:
  - [`Options`](#Options)
  - [`Params`](#Params): [`Params.Request`](#Params.Request)
  - [`Request`](#Request): [`Parse`](#Parse)
  - [`Result`](#Result)
  - [`SortField`](#SortField)

## Constants

<a id="DefaultLimit"></a>
<a id="MaxLimit"></a>
<a id="MaxCursorLength"></a>

```go
const (
	DefaultLimit = 20
	MaxLimit     = 100
	// MaxCursorLength bounds cursor size to limit abuse.
	MaxCursorLength = 512
)
```

Limits used when [Options](#Options) leaves them zero.

*Since `v0.1.0`*

## Variables

<a id="ErrInvalidLimit"></a>
<a id="ErrInvalidCursor"></a>
<a id="ErrInvalidSort"></a>

```go
var (
	ErrInvalidLimit  = errors.New("page: invalid limit")
	ErrInvalidCursor = errors.New("page: invalid cursor")
	ErrInvalidSort   = errors.New("page: invalid sort")
)
```

Errors returned for invalid pagination input.

*Since `v0.1.0`*

## Functions

<a id="DecodeCursor"></a>

### func DecodeCursor

```go
func DecodeCursor(cursor string, position any) error
```

DecodeCursor decodes a cursor produced by [EncodeCursor](#EncodeCursor) into position.

*Since `v0.1.0`*

<a id="EncodeCursor"></a>

### func EncodeCursor

```go
func EncodeCursor(position any) (string, error)
```

EncodeCursor returns an opaque cursor for position, which must be JSON-encodable.

*Since `v0.1.0`*

## Types

<a id="Options"></a>
<a id="Options.DefaultLimit"></a>
<a id="Options.MaxLimit"></a>
<a id="Options.SortFields"></a>
<a id="Options.DefaultSort"></a>

### type Options

```go
type Options struct {
	DefaultLimit int // zero means DefaultLimit
	MaxLimit     int // zero means MaxLimit
	// SortFields is the allowlist of sortable fields. Empty means sorting is not allowed.
	SortFields []string
	// DefaultSort applies when the request has no sort.
	DefaultSort []SortField
}
```

Options configure what a list endpoint accepts.

*Since `v0.1.0`*

<a id="Params"></a>
<a id="Params.Limit"></a>
<a id="Params.Cursor"></a>
<a id="Params.Sort"></a>

### type Params

```go
type Params struct {
	Limit  int    `query:"limit" minimum:"0" maximum:"100" doc:"Maximum number of items to return (default 20, max 100)"`
	Cursor string `query:"cursor" maxLength:"512" doc:"Opaque cursor from a previous response's next_cursor"`
	Sort   string `query:"sort" maxLength:"200" doc:"Comma-separated sort fields; prefix a field with - for descending order" example:"-created_at"`
}
```

Params are the raw query parameters of a list endpoint. The struct tags make Huma document and validate them.

*Since `v0.1.0`*

<a id="Params.Request"></a>

#### func (Params) Request

```go
func (p Params) Request(opts Options) (Request, error)
```

Request validates p against opts.

*Since `v0.1.0`*

<a id="Request"></a>
<a id="Request.Limit"></a>
<a id="Request.Cursor"></a>
<a id="Request.Sort"></a>

### type Request

```go
type Request struct {
	Limit  int
	Cursor string
	Sort   []SortField
}
```

Request is a validated pagination request.

*Since `v0.1.0`*

<a id="Parse"></a>

#### func Parse

```go
func Parse(q url.Values, opts Options) (Request, error)
```

Parse reads limit, cursor and sort from URL query values.

*Since `v0.1.0`*

<a id="Result"></a>
<a id="Result.Items"></a>
<a id="Result.NextCursor"></a>

### type Result

```go
type Result[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}
```

Result is a page of items. NextCursor is empty on the last page.

*Since `v0.1.0`*

<a id="SortField"></a>
<a id="SortField.Field"></a>
<a id="SortField.Desc"></a>

### type SortField

```go
type SortField struct {
	Field string
	Desc  bool
}
```

SortField is one validated sort key.

*Since `v0.1.0`*
