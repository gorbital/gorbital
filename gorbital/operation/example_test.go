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
