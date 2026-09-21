# gorbital/flagshttp

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/flagshttp"
```

Package flagshttp is the client feature flags API as a gorbital module: GET /v1/flags tells a signed-in caller whether each flag declared with flags.Client() is on for them (ADR-0057). Operators change flags through the operations API (gorbital.dev/gorbital/opshttp).

Its path, operation ID, response schema, error codes and permission are those of the flags module a v0.1 app generated, and are public API (ADR-0015).

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Constants: [`PermRead`](#PermRead)
- Functions: [`Module`](#Module)

## Constants

<a id="PermRead"></a>

```go
const PermRead = flagsusecase.PermFlagsRead
```

PermRead lets a caller read the client flags. The module grants it to the user role, which every signed-in user holds; an API key needs it in its scopes (ADR-0058). Permission names are public API.

*Since `v0.2.2 (unreleased)`*

## Functions

<a id="Module"></a>

### func Module

```go
func Module() gorbital.Module
```

Module returns the client feature flags API. Its name is "flags".

*Since `v0.2.2 (unreleased)`*

**Example**

```go
// cmd/api/main.go of an app whose clients read their feature flags.
main := func() {
	gorbital.Main(gorbital.WithModules(flagshttp.Module()))
}
_ = main

mux := http.NewServeMux()
api := openapi.New(mux, "acme-api", "1.0.0", openapi.WithBearerAuth("Session token"))
mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
if err != nil {
	panic(err)
}
openapi.InstallErrors(mapper)
m := flagshttp.Module()
if err := gorbital.Mount(api, mapper, gorbital.Deps{}, m); err != nil {
	panic(err)
}
fmt.Println(m.Permissions[0].Name, m.Permissions[0].Roles)

rec := httptest.NewRecorder()
mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/flags", nil))
fmt.Println(rec.Code)
```

Output:

```text
flags.flag.read [user]
401
```
