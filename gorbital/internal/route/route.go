// Package route holds the route configuration that the gorbital and guard
// packages both set, so guards can be route options without the gorbital
// package exporting its internals.
package route

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/ratelimit"
)

// Config is what a route's options set before it is registered.
type Config struct {
	Tags        []string
	Summary     string
	Description string
	OperationID string
	// Status is the success status; zero keeps Huma's default.
	Status int
	// Errors are error statuses the route documents.
	Errors     []int
	Deprecated bool
	// Public removes the authenticated-actor check and the security
	// requirement (ADR-0082).
	Public bool
	// AuthenticateAfterInput moves a non-public route's authenticated-actor
	// check from before input parsing to after input validation, just before
	// the handler, so invalid input is answered first (gorbital's
	// AuthenticateAfterInput option).
	AuthenticateAfterInput bool
	// Middlewares run in order around the guards and the handler.
	Middlewares []func(http.Handler) http.Handler
	// Guards run in order after the middlewares, before input parsing.
	Guards []Guard
	// Customize change the Huma operation before it is registered, in
	// order.
	Customize []func(api huma.API, op *huma.Operation)
}

// Option sets part of a route's configuration.
type Option func(*Config)

// A Guard decides whether a request reaches the handler.
type Guard struct {
	// Name identifies the guard in errors, metrics and the OpenAPI
	// document, such as "permission:books.book.write".
	Name string
	// Statuses are the error statuses the guard answers with.
	Statuses []int
	// Check returns nil to allow the request, or the error to refuse it with.
	Check func(ctx huma.Context) error
	// Limit, when set, is resolved to a limiter at registration and
	// checked instead of Check.
	Limit *Limit
	// Scope, when set, makes the guard check membership of the app's scope
	// with the app's scope authorizer instead of Check (guard.Scope).
	Scope *Scope
	// Err makes registration fail, for a guard built with invalid arguments.
	Err error
}

// OrgIDParam is the path parameter an app that has not named its scope
// reads the scope ID from. It is the default of gorbital.Scope.PathParam,
// and the parameter every v0.1 and v0.2 organisation route uses.
const OrgIDParam = "orgId"

// Scope is the membership check of guard.Scope.
type Scope struct {
	// Permission is the permission the member's role must grant.
	Permission string
}

// Limit is a rate limit a guard applies.
type Limit struct {
	// Name is the limiter's name; empty uses the operation ID.
	Name  string
	Limit ratelimit.Limit
	// Key returns the key a request is counted under.
	Key func(ctx context.Context, hctx huma.Context) string
	// Keys says what a key is, such as "actor", for /ops/auth/rate-limits.
	Keys string
}
