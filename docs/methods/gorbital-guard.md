# gorbital/guard

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/guard"
```

Package guard provides route options that decide whether a request may reach a route's handler (ADR-0082). Every route requires an authenticated actor unless it has [Public](#Public); guards run before the request's input is parsed, and document what they refuse in the OpenAPI document.

```go
gorbital.Get(books, "/{id}", h.getBook)                      // signed-in callers only
gorbital.Get(r, "/v1/catalog", h.listCatalog, guard.Public()) // anyone
```

Stability: experimental until v0.2.0 (ADR-0015, ADR-0082).

## Contents

- Functions: [`Public`](#Public)

## Functions

<a id="Public"></a>

### func Public

```go
func Public() gorbital.RouteOption
```

Public lets requests without an authenticated actor reach the route, and removes its security requirement from the OpenAPI document. On a group, it applies to every route in the group.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
package guard_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

type catalogInput struct {
	ID string `path:"id"`
}

type catalogEntry struct {
	Body struct {
		Title string `json:"title"`
	}
}

func catalogBook(context.Context, *catalogInput) (*catalogEntry, error) {
	out := &catalogEntry{}
	out.Body.Title = "Dune"
	return out, nil
}

func ExamplePublic() {
	mux := http.NewServeMux()
	api := openapi.New(mux, "Shelfie", "1.0.0", openapi.WithBearerAuth("Session token"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		panic(err)
	}
	openapi.InstallErrors(mapper)

	err = gorbital.Mount(api, mapper, gorbital.Deps{}, gorbital.Module{
		Name: "catalog",
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			public := r.Group("/v1/catalog", guard.Public())
			gorbital.Get(public, "/{id}", catalogBook)
			gorbital.Get(r, "/v1/wishlist/{id}", catalogBook)
		},
	})
	if err != nil {
		panic(err)
	}

	for _, target := range []string{"/v1/catalog/bok_1", "/v1/wishlist/bok_1"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		fmt.Println(target, rec.Code)
	}
	// Output:
	// /v1/catalog/bok_1 200
	// /v1/wishlist/bok_1 401
}
```

Output:

```text
/v1/catalog/bok_1 200
/v1/wishlist/bok_1 401
```
