package delivery

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
)

// The documented password rules, as format strings taking the shortest
// password accepted. The request bodies carry them in `doc` struct tags
// filled in with v0.1's 12 characters, so an app that doesn't raise the
// minimum gets v0.1's document byte for byte (TestOpenAPIMatchesV010).
// TestPasswordMinimumInOpenAPI reads both wordings out of the document, so
// a tag and its format here can't drift apart.
const (
	passwordDocFormat    = "At least %d characters" //nolint:gosec // documentation, not a credential
	newPasswordDocFormat = "The new password, at least %d characters"
)

// A passwordField is a request body property stating the shortest password
// accepted, with the format its description is written with.
type passwordField struct{ name, format string }

// passwordRoute registers op like [route] and then states rs.minPassword in
// the description of fields, which the struct tags carry as v0.1's minimum.
//
// The description can't come from the struct tag, nor from a TransformSchema
// method like RegisterBody's: both belong to a Go type, one per process,
// while the minimum belongs to an app, and a test binary builds several apps
// with different minimums at once. So the operation is registered first and
// the schema Huma put in this API's registry is rewritten afterwards. Every
// app has its own registry, and modules register their routes one at a time
// before the app serves anything (gorbital.Mount), so no other goroutine can
// be reading the schema.
//
// Only the description changes. A minLength would move a short password from
// the documented 422 weak_password, which names the minimum and runs the
// app's PasswordPolicy checks, to Huma's generic validation error, and would
// do so only for apps that raised the minimum.
func passwordRoute[I, O any](rs *routes, op huma.Operation, handler func(context.Context, *I) (*O, error), fields ...passwordField) {
	var api huma.API
	route(rs, op, handler, gorbital.Customize(func(a huma.API, _ *huma.Operation) { api = a }))
	if api == nil || rs.minPassword == 0 {
		return
	}
	describePasswords(api, op, rs.minPassword, fields)
}

// describePasswords sets the description of each of fields in the JSON
// request body schema of the registered operation op to its format filled in
// with minimum. It panics when the schema isn't where it expects it, which
// gorbital.Mount reports as a registration error rather than shipping a
// document that understates the minimum.
func describePasswords(api huma.API, op huma.Operation, minimum int, fields []passwordField) {
	s := requestBodySchema(api, op)
	if s == nil {
		panic(fmt.Sprintf("authhttp: %s %s: no JSON request body schema to document the password minimum on", op.Method, op.Path))
	}
	for _, f := range fields {
		p := s.Properties[f.name]
		if p == nil {
			panic(fmt.Sprintf("authhttp: %s %s: no %q property to document the password minimum on", op.Method, op.Path, f.name))
		}
		p.Description = fmt.Sprintf(f.format, minimum)
	}
}

// requestBodySchema returns the application/json request body schema Huma
// registered for op, resolving the reference to the schema in the API's
// registry. It returns nil when op has no such body.
func requestBodySchema(api huma.API, op huma.Operation) *huma.Schema {
	item := api.OpenAPI().Paths[op.Path]
	if item == nil {
		return nil
	}
	var registered *huma.Operation
	switch op.Method {
	case http.MethodPost:
		registered = item.Post
	case http.MethodPut:
		registered = item.Put
	case http.MethodPatch:
		registered = item.Patch
	}
	if registered == nil || registered.RequestBody == nil {
		return nil
	}
	media := registered.RequestBody.Content["application/json"]
	if media == nil || media.Schema == nil {
		return nil
	}
	if ref := media.Schema.Ref; ref != "" {
		return api.OpenAPI().Components.Schemas.SchemaFromRef(ref)
	}
	return media.Schema
}
