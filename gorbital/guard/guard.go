// Package guard provides route options that decide whether a request may
// reach a route's handler (ADR-0082). Every route requires an authenticated
// actor unless it has [Public]; guards run before the request's input is
// parsed, and document what they refuse in the OpenAPI document.
//
//	gorbital.Get(books, "/{id}", h.getBook)                      // signed-in callers only
//	gorbital.Get(r, "/v1/catalog", h.listCatalog, guard.Public()) // anyone
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0082).
package guard

import (
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/internal/route"
)

// Public lets requests without an authenticated actor reach the route, and
// removes its security requirement from the OpenAPI document. On a group, it
// applies to every route in the group.
func Public() gorbital.RouteOption {
	return func(c *route.Config) { c.Public = true }
}
