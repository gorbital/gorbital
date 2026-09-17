// Package operation registers operations declared as huma.Operation values,
// as v0.1 apps declare them for huma.Register, on a gorbital.Router.
//
// The built-in modules (opshttp, flagshttp, mailevents, orgshttp) keep v0.1's
// declarations, so their move into the library reads line by line and their
// OpenAPI compares with the frozen v0.1.0 documents (ADR-0083). A module
// ejected with orb eject keeps them too, and code moved from a v0.1 app can
// replace huma.Register(api, op, handler) with Register(r, op, handler).
// New code uses gorbital.Get, gorbital.Post and the other verbs with route
// options instead.
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0083).
package operation

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/modules/openapi"
)

// Register registers handler for op on r with the route options op's fields
// translate to: OperationID, Summary, Description, Tags, Errors and Status,
// and Responses, MaxBodyBytes and SkipValidateBody through gorbital.Customize.
// An op with the bearer security requirement (openapi.Bearer) requires an
// authenticated actor, as every gorbital route does unless it is public; an
// op without security requirements is guard.Public(). extra options apply
// after those, such as a guard or a gorbital.Customize that adds schemas to
// the API's registry.
//
// Register panics, which gorbital.Mount reports as an error naming the
// module, for an op with a field it doesn't carry over (such as Hidden or
// Middlewares), another security requirement, or a method other than GET,
// POST, PUT, PATCH and DELETE: dropping them silently would change the
// operation.
func Register[I, O any](r *gorbital.Router, op huma.Operation, handler func(context.Context, *I) (*O, error), extra ...gorbital.RouteOption) {
	opts := []gorbital.RouteOption{
		gorbital.OperationID(op.OperationID),
		gorbital.Summary(op.Summary),
		gorbital.Description(op.Description),
		gorbital.Tags(op.Tags...),
		gorbital.Errors(op.Errors...),
	}
	if op.DefaultStatus != 0 {
		opts = append(opts, gorbital.Status(op.DefaultStatus))
	}
	if len(op.Security) == 0 {
		opts = append(opts, guard.Public())
	}
	if op.Responses != nil || op.MaxBodyBytes != 0 || op.SkipValidateBody {
		opts = append(opts, gorbital.Customize(func(_ huma.API, o *huma.Operation) {
			o.Responses, o.MaxBodyBytes, o.SkipValidateBody = op.Responses, op.MaxBodyBytes, op.SkipValidateBody
		}))
	}
	opts = append(opts, extra...)
	if err := unsupported(op); err != nil {
		panic(err)
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
		panic(fmt.Sprintf("operation %s: method %q isn't supported", op.OperationID, op.Method))
	}
}

// carried are the huma.Operation fields Register carries over.
var carried = []string{
	"OperationID", "Method", "Path", "Summary", "Description", "Tags", "Security", "Errors",
	"DefaultStatus", "Responses", "MaxBodyBytes", "SkipValidateBody",
}

// unsupported returns an error naming a field of op that is set and that
// Register would drop.
func unsupported(op huma.Operation) error {
	v := reflect.ValueOf(op)
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if !f.IsExported() || slices.Contains(carried, f.Name) || v.Field(i).IsZero() {
			continue
		}
		return fmt.Errorf("operation %s: the %s field isn't carried over", op.OperationID, f.Name)
	}
	if len(op.Security) > 0 && fmt.Sprint(op.Security) != fmt.Sprint(openapi.Bearer) {
		return fmt.Errorf("operation %s: only the bearer security requirement is carried over", op.OperationID)
	}
	return nil
}
