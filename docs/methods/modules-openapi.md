# modules/openapi

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/modules/openapi"
```

Package openapi integrates Huma with gorbital (ADR-0027): an API on the standard http.ServeMux, problem+json errors produced by the application's error mapper, an API reference at /docs in the gorbital design (ADR-0049, rendered by package reference), and OpenAPI export.

Huma is used only in delivery layers and the composition root of generated apps; domain, use case and repository code never imports it.

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Constants: [`BearerScheme`](#BearerScheme)
- Variables: [`Bearer`](#Bearer)
- Functions: [`InstallErrors`](#InstallErrors), [`MountDocs`](#MountDocs), [`New`](#New), [`WriteSpec`](#WriteSpec)
- Types:
  - [`DocsOptions`](#DocsOptions)
  - [`Incompatibility`](#Incompatibility): [`CheckCompatible`](#CheckCompatible), [`Incompatibility.String`](#Incompatibility.String)
  - [`Option`](#Option): [`WithBearerAuth`](#WithBearerAuth), [`WithDescription`](#WithDescription), [`WithoutSpecEndpoints`](#WithoutSpecEndpoints)

## Constants

<a id="BearerScheme"></a>

```go
const BearerScheme = "bearer"
```

BearerScheme is the name of the bearer security scheme.

*Since `v0.1.0`*

## Variables

<a id="Bearer"></a>

```go
var Bearer = []map[string][]string{{BearerScheme: {}}}
```

Bearer is the security requirement for operations that need a session token. Use it as huma.Operation.Security.

*Since `v0.1.0`*

## Functions

<a id="InstallErrors"></a>

### func InstallErrors

```go
func InstallErrors(mapper *httpx.Mapper)
```

InstallErrors makes every Huma error an [httpx.Problem](httpx.md#Problem):

  - errors matched by mapper (domain sentinels, \*httpx.Problem) use their mapping;
  - validation and other client errors keep their status, get the default code (for example "validation\_failed") and list field errors without echoing submitted values;
  - server errors become a generic 500 and are logged once by mapper.

Huma stores these hooks in package-level variables, so call InstallErrors once, from the composition root, before registering operations. It is the only package-level state an gorbital app changes (ADR-0027).

*Since `v0.1.0`*

<a id="MountDocs"></a>

### func MountDocs

```go
func MountDocs(mux *http.ServeMux, opts DocsOptions)
```

MountDocs serves an API reference for the API on mux at opts.Path, in the gorbital design (ADR-0049): an overview, a page per operation with its parameters, responses, request examples and "Try it", and search.

The pages are rendered from the OpenAPI document that mux serves at opts.SpecURL, read in-process on the first request, so every operation registered before the server starts appears without further setup. Styles, scripts and fonts are served by the app itself under [reference.ContentSecurityPolicy](modules-openapi-reference.md#ContentSecurityPolicy); the pages make no external requests.

*Since `v0.1.0`*

<a id="New"></a>

### func New

```go
func New(mux *http.ServeMux, title, version string, opts ...Option) huma.API
```

New returns a Huma API registered on mux. It serves the OpenAPI document at /openapi.json and /openapi.yaml (unless [WithoutSpecEndpoints](#WithoutSpecEndpoints)), disables Huma's built-in docs (use [MountDocs](#MountDocs)) and response $schema links, and doesn't expose /schemas.

Call [InstallErrors](#InstallErrors) before registering operations.

*Since `v0.1.0`*

<a id="WriteSpec"></a>

### func WriteSpec

```go
func WriteSpec(w io.Writer, api huma.API) error
```

WriteSpec writes api's OpenAPI document to w as indented JSON. Generated apps call it from "my-api openapi" to export api/openapi.json.

*Since `v0.1.0`*

## Types

<a id="DocsOptions"></a>
<a id="DocsOptions.Path"></a>
<a id="DocsOptions.SpecURL"></a>
<a id="DocsOptions.Title"></a>

### type DocsOptions

```go
type DocsOptions struct {
	Path    string // default "/docs"
	SpecURL string // default "/openapi.json", served by the same mux
	Title   string // default "API Reference"
}
```

DocsOptions configure [MountDocs](#MountDocs).

*Since `v0.1.0`*

<a id="Incompatibility"></a>
<a id="Incompatibility.Operation"></a>
<a id="Incompatibility.Where"></a>
<a id="Incompatibility.Change"></a>

### type Incompatibility

```go
type Incompatibility struct {
	// Operation is the method and path, such as "GET /ops/settings/{key}".
	Operation string
	// Where locates the change inside the operation, such as
	// "response 200 application/json: items[].key". Empty for the operation
	// itself.
	Where string
	// Change says what changed, such as "removed".
	Change string
}
```

An Incompatibility is a difference between two OpenAPI documents that can break a client written against the older one (ADR-0054).

*Since `v0.1.0`*

<a id="CheckCompatible"></a>

#### func CheckCompatible

```go
func CheckCompatible(baseline, current []byte, prefix string) ([]Incompatibility, error)
```

CheckCompatible compares the operations under prefix (such as "/ops/") in a baseline OpenAPI 3 JSON document with the current one and returns every change that can break existing clients, sorted. Additions are compatible. It reports:

  - a removed path or method (path parameters may be renamed);
  - a removed parameter, or one that became required;
  - a request body, media type or property that was removed, a property or body that became required, a request type that accepts fewer types, or an enum that allows fewer values;
  - a removed response status or media type, a removed response property, one that is no longer always present, or a response type that may return types it didn't before;
  - authentication required by an operation that had none.

Schemas are compared through $ref, nested objects, array items and map values. Descriptions, examples, bounds and formats aren't compared. Generated apps check /ops/\* against api/openapi.baseline.json; apps can check their own API the same way.

*Since `v0.1.0`*

<a id="Incompatibility.String"></a>

#### func (Incompatibility) String

```go
func (i Incompatibility) String() string
```

String formats the incompatibility as one line.

*Since `v0.1.0`*

<a id="Option"></a>

### type Option

```go
type Option interface {
	// contains filtered or unexported methods
}
```

An Option configures [New](#New).

*Since `v0.1.0`*

<a id="WithBearerAuth"></a>

#### func WithBearerAuth

```go
func WithBearerAuth(description string) Option
```

WithBearerAuth declares the bearer security scheme used by [Bearer](#Bearer).

*Since `v0.1.0`*

<a id="WithDescription"></a>

#### func WithDescription

```go
func WithDescription(markdown string) Option
```

WithDescription sets the API description shown in the docs.

*Since `v0.1.0`*

<a id="WithoutSpecEndpoints"></a>

#### func WithoutSpecEndpoints

```go
func WithoutSpecEndpoints() Option
```

WithoutSpecEndpoints stops [New](#New) from serving the OpenAPI document, for APIs whose contract isn't public. [WriteSpec](#WriteSpec) still exports it; [MountDocs](#MountDocs), which reads the served document, can't be used with it.

*Since `v0.1.0`*
