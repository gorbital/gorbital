package delivery

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	gorbitalroute "gorbital.dev/gorbital/internal/route"
)

// route registers op on r with handler, carrying over every field v0.1's
// huma.Register calls set: the operation ID, method, path, tags, summary,
// description, success status and error statuses. An operation without
// op.Security is public. One with it requires sign-in in the OpenAPI
// document, and its use case refuses a request without an actor after the
// input is validated, as in v0.1 (route.Config.ActorCheckedByHandler).
//
// Any other Operation field would be dropped silently, so route panics on
// one; gorbital.Mount reports the panic as a registration error.
func route[I, O any](rs *routes, op huma.Operation, handler func(context.Context, *I) (*O, error)) {
	r := rs.router(op.Path)
	if err := unsupported(op); err != nil {
		panic(fmt.Sprintf("authhttp: %s %s: %v", op.Method, op.Path, err))
	}
	opts := []gorbital.RouteOption{
		gorbital.OperationID(op.OperationID), gorbital.Tags(op.Tags...), gorbital.Summary(op.Summary),
		gorbital.Description(op.Description), gorbital.Errors(op.Errors...),
	}
	if op.DefaultStatus != 0 {
		opts = append(opts, gorbital.Status(op.DefaultStatus))
	}
	if op.Security == nil {
		opts = append(opts, guard.Public())
	} else {
		opts = append(opts, func(c *gorbitalroute.Config) { c.ActorCheckedByHandler = true })
	}
	switch op.Method {
	case http.MethodGet:
		gorbital.Get(r, op.Path, handler, opts...)
	case http.MethodPost:
		gorbital.Post(r, op.Path, handler, opts...)
	case http.MethodPut:
		gorbital.Put(r, op.Path, handler, opts...)
	case http.MethodPatch:
		gorbital.Patch(r, op.Path, handler, opts...)
	case http.MethodDelete:
		gorbital.Delete(r, op.Path, handler, opts...)
	default:
		panic(fmt.Sprintf("authhttp: %s %s: unsupported method", op.Method, op.Path))
	}
}

// routes are the routers sign-in's operations are registered on: signIn
// for /v1/auth/ (with authhttp's RouteMiddleware), base for the rest.
type routes struct {
	base, signIn *gorbital.Router
}

// routesOn registers every operation on r.
func routesOn(r *gorbital.Router) *routes { return &routes{base: r, signIn: r} }

// router returns the router for an operation's path.
func (rs *routes) router(path string) *gorbital.Router {
	if strings.HasPrefix(path, "/v1/auth/") {
		return rs.signIn
	}
	return rs.base
}

// unsupported reports an Operation field route doesn't carry over.
func unsupported(op huma.Operation) error {
	switch {
	case op.OperationID == "" || op.Summary == "":
		return errors.New("operation ID and summary are required")
	case op.Hidden || op.Deprecated || op.RequestBody != nil || op.Responses != nil || op.Parameters != nil ||
		op.Callbacks != nil || op.Servers != nil || op.ExternalDocs != nil || op.Extensions != nil || op.Metadata != nil ||
		op.Middlewares != nil || op.MaxBodyBytes != 0 || op.BodyReadTimeout != 0 || op.SkipValidateBody || op.SkipValidateParams:
		return errors.New("an operation field authhttp's routes don't carry over is set")
	}
	return nil
}
