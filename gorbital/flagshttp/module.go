// Package flagshttp is the client feature flags API as a gorbital module:
// GET /v1/flags tells a signed-in caller whether each flag declared with
// flags.Client() is on for them (ADR-0057). Operators change flags through
// the operations API (gorbital.dev/gorbital/opshttp).
//
// Its path, operation ID, response schema, error codes and permission are
// those of the flags module a v0.1 app generated, and are public API
// (ADR-0015).
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).
package flagshttp

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"gorbital.dev/gorbital/flagshttp/internal/delivery"
	flagsdomain "gorbital.dev/gorbital/flagshttp/internal/domain"
	flagsusecase "gorbital.dev/gorbital/flagshttp/internal/usecase"
)

// PermRead lets a caller read the client flags. The module grants it to
// the user role, which every signed-in user holds; an API key needs it in
// its scopes (ADR-0058). Permission names are public API.
const PermRead = flagsusecase.PermFlagsRead

// roleUser is the role every signed-in user holds without a grant.
const roleUser = "user"

// Module returns the client feature flags API. Its name is "flags".
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "flags",
		// Error codes are public API: add new ones, never change existing
		// ones.
		Errors: []httpx.Mapping{
			{Err: flagsdomain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: flagsdomain.ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Detail: "missing permission for this operation"},
		},
		Permissions: []gorbital.Permission{
			{Name: PermRead, Description: "Read the feature flags shown to clients", Roles: []string{roleUser}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			var store flagsusecase.FlagsStore
			if d.Flags != nil {
				store = d.Flags
			}
			delivery.Register(r, flagsusecase.NewService(store))
		},
	}
}
