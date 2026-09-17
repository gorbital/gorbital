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
	// ActorCheckedByHandler keeps a non-public route's security requirement
	// but leaves the authenticated-actor check to its use case, which then
	// answers 401 after input validation instead of before it. Only
	// gorbital.dev/gorbital/authhttp sets it, so the sign-in endpoints keep
	// v0.1's order of responses (ADR-0083, Phase 5 notes); no public option
	// exposes it.
	ActorCheckedByHandler bool
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
	// Org, when set, makes the guard check organisation membership with the
	// app's organisation authorizer instead of Check (guard.OrgMember).
	Org *Org
	// Err makes registration fail, for a guard built with invalid arguments.
	Err error
}

// OrgIDParam is the path parameter guard.OrgMember reads the organisation
// ID from.
const OrgIDParam = "orgId"

// Org is the membership check of guard.OrgMember.
type Org struct {
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
