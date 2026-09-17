# buildinfo

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/buildinfo"
```

Package buildinfo reports the version, commit and build time of the running binary, from Go build information or a version set at link time.

Set an explicit version when building:

```
go build -ldflags "-X gorbital.dev/buildinfo.version=v1.2.3" ./cmd/api
```

Stability: stable (ADR-0015, ADR-0054).

## Contents

- Functions: [`Handler`](#Handler)
- Types:
  - [`Info`](#Info): [`Read`](#Read)

## Functions

<a id="Handler"></a>

### func Handler

```go
func Handler() http.Handler
```

Handler serves [Read](#Read) as JSON.

*Since `v0.1.0`*

## Types

<a id="Info"></a>
<a id="Info.Version"></a>
<a id="Info.Commit"></a>
<a id="Info.BuildTime"></a>
<a id="Info.Modified"></a>
<a id="Info.GoVersion"></a>

### type Info

```go
type Info struct {
	Version   string `json:"version" example:"v1.2.3"`
	Commit    string `json:"commit,omitempty" example:"3f9a1c2b7d4e"`
	BuildTime string `json:"build_time,omitempty" example:"2026-09-14T12:00:00Z"`
	Modified  bool   `json:"modified,omitempty"`
	GoVersion string `json:"go_version" example:"go1.25.3"`
}
```

Info describes the running binary.

*Since `v0.1.0`*

<a id="Read"></a>

#### func Read

```go
func Read() Info
```

Read returns information about the running binary. Version is "dev" when neither a link-time version nor a module version is available.

*Since `v0.1.0`*
