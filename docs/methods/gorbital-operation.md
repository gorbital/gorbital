# gorbital/operation

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/operation"
```

Package operation registers operations declared as huma.Operation values, as v0.1 apps declare them for huma.Register, on a gorbital.Router.

The built-in modules (opshttp, flagshttp, mailevents, orgshttp) keep v0.1's declarations, so their move into the library reads line by line and their OpenAPI compares with the frozen v0.1.0 documents (ADR-0083). A module the app holds as its own code keeps them too, and code moved from a v0.1 app can replace huma.Register(api, op, handler) with Register(r, op, handler). New code uses gorbital.Get, gorbital.Post and the other verbs with route options instead.

Stability: experimental until v0.2.0 (ADR-0015, ADR-0083).

## Contents

- Functions: [`Register`](#Register)

## Functions

<a id="Register"></a>

### func Register

```go
func Register[I, O any](r *gorbital.Router, op huma.Operation, handler func(context.Context, *I) (*O, error), extra ...gorbital.RouteOption)
```

Register registers handler for op on r with the route options op's fields translate to: OperationID, Summary, Description, Tags, Errors and Status, and Responses, MaxBodyBytes and SkipValidateBody through gorbital.Customize. An op with the bearer security requirement (openapi.Bearer) requires an authenticated actor, as every gorbital route does unless it is public; an op without security requirements is guard.Public(). extra options apply after those, such as a guard or a gorbital.Customize that adds schemas to the API's registry.

Register panics, which gorbital.Mount reports as an error naming the module, for an op with a field it doesn't carry over (such as Hidden or Middlewares), another security requirement, or a method other than GET, POST, PUT, PATCH and DELETE: dropping them silently would change the operation.

*Since `v0.2.2 (unreleased)`*

**Example**

A v0.1 declaration, registered on a module's router: the operation keeps its ID, summary, tags and bearer security, so it requires sign-in.

```go
package operation_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/operation"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
)

type pingOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

// A v0.1 declaration, registered on a module's router: the operation keeps
// its ID, summary, tags and bearer security, so it requires sign-in.
func ExampleRegister() {
	ping := huma.Operation{
		OperationID: "ops-ping", Method: http.MethodGet, Path: "/ops/ping",
		Summary: "Check the operations API", Tags: []string{"Ops"}, Security: openapi.Bearer,
	}
	module := gorbital.Module{
		Name: "ops",
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			operation.Register(r, ping, func(context.Context, *struct{}) (*pingOutput, error) {
				out := &pingOutput{}
				out.Body.Message = "pong"
				return out, nil
			})
		},
	}

	api := openapi.New(http.NewServeMux(), "example", "1.0.0", openapi.WithBearerAuth("token"))
	mapper, err := httpx.NewMapper(slog.New(slog.DiscardHandler))
	if err != nil {
		panic(err)
	}
	if err := gorbital.Mount(api, mapper, gorbital.Deps{}, module); err != nil {
		panic(err)
	}
	op := api.OpenAPI().Paths["/ops/ping"].Get
	fmt.Println(op.OperationID, op.Summary, len(op.Security) > 0)
	// Output: ops-ping Check the operations API true
}
```

Output:

```text
ops-ping Check the operations API true
```
