// Package guard provides route options that decide whether a request may
// reach a route's handler (ADR-0082). Every route requires an authenticated
// actor unless it has [Public]; the guards below add to that. Guards run in
// the order declared, group guards first, after the route's middleware and
// before its input is parsed, so a refused request's body is never read.
// Each guard documents the responses it refuses with in the OpenAPI document.
//
//	books := r.Group("/v1/books", gorbital.Tags("Books"))
//	gorbital.Get(books, "/{id}", h.getBook, guard.Permission("books.book.read"))
//	gorbital.Post(books, "", h.createBook,
//		guard.Permission("books.book.write"),
//		guard.RateLimit(30, time.Minute))
//	gorbital.Get(r, "/v1/catalog/{id}", h.catalogBook, guard.Public())
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0082).
package guard

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/internal/route"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/auth"
	"gorbital.dev/ratelimit"
)

// Public lets requests without an authenticated actor reach the route, and
// removes its security requirement from the OpenAPI document. On a group, it
// applies to every route in the group.
func Public() gorbital.RouteOption {
	return func(c *route.Config) { c.Public = true }
}

// The refusals of the built-in guards. Their codes are the ones v0.1
// endpoints return, except reauthentication_required, new in v0.2.
var (
	errForbidden              = httpx.NewProblem(http.StatusForbidden, "forbidden", "missing permission for this operation")
	errMFARequired            = httpx.NewProblem(http.StatusForbidden, "mfa_required", "the permission needs a session signed in with a second factor: turn on two-factor authentication and sign in again")
	errUnauthenticated        = httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "authentication is required")
	errReauthenticationNeeded = httpx.NewProblem(http.StatusForbidden, "reauthentication_required", "sign in again, or verify your second factor, to continue")
	errSessionRequired        = httpx.NewProblem(http.StatusForbidden, "session_required", "the operation needs the person's signed-in session, not an API key")
)

// Permission refuses callers without permission: 403 forbidden, or 403
// mfa_required when the caller's roles grant it only to a session signed in
// with a second factor. API keys hold a permission only when their scopes
// include it.
func Permission(name string) gorbital.RouteOption {
	return addGuard(route.Guard{
		Name:     "permission:" + name,
		Statuses: []int{http.StatusForbidden},
		Check: func(hctx huma.Context) error {
			switch err := actor.Require(hctx.Context(), name); {
			case err == nil:
				return nil
			case errors.Is(err, actor.ErrStepUpRequired):
				return errMFARequired
			case errors.Is(err, actor.ErrUnauthenticated):
				return errUnauthenticated
			default:
				return errForbidden
			}
		},
		Err: validName("permission", name),
	})
}

// RecentReauth refuses a session that neither signed in nor verified a
// second factor within auth.RecentVerification (10 minutes), with 403
// reauthentication_required, and refuses API keys with 403
// session_required. Use it on operations that change how an account signs
// in or that an attacker holding a stolen session shouldn't reach.
func RecentReauth() gorbital.RouteOption {
	return addGuard(route.Guard{
		Name:     "recent_reauth",
		Statuses: []int{http.StatusForbidden},
		Check: func(hctx huma.Context) error {
			p, ok := auth.PrincipalFrom(hctx.Context())
			switch {
			case !ok:
				return errUnauthenticated
			case p.SessionID == "":
				return errSessionRequired
			case p.RecentlySignedIn(time.Now()) || p.RecentlyVerified(time.Now()):
				return nil
			default:
				return errReauthenticationNeeded
			}
		},
	})
}

// Scope refuses callers who aren't members of the scope in the route's
// path, with a role granting permission (ADR-0088). The scope is the app's
// tenancy: organisations by default, or whatever the app named with
// gorbital.WithScope — merchants, clinics, restaurants. The path parameter
// is the scope's PathParam, "orgId" unless the app changed it, so routes
// look like /v1/orgs/{orgId}/… or /v1/merchants/{merchantId}/….
//
// It asks the app's scope authorizer on every request:
//
//   - 404 with the scope's refusal code ("org_not_found" by default) when
//     the scope doesn't exist, is deleted, has a malformed ID, or the
//     caller isn't a member: the four look the same, so scope IDs can't be
//     probed;
//   - 403 mfa_required when the member's role grants permission only to a
//     session signed in with a second factor, never to API keys;
//   - 403 forbidden when the role doesn't grant it.
//
// Members are users with a session, their API keys (within the keys'
// scopes), and the scope's own service accounts through their keys; a
// service account never reaches another scope. Platform roles grant
// nothing in a scope.
//
// On success, the actor acts in the scope: its OrgID is set and its
// permissions are those of the member's role, so audit events carry the
// scope, guards after it such as [Permission] check scope permissions, and
// the request's database connections carry it for row-level security
// (gorbital.Scope.Session, ADR-0061). Declare the permission with
// gorbital.Permission.ScopeRoles.
//
// Registration fails when the path has no scope parameter or the route is
// public, and gorbital.New fails when the app has no scope.
func Scope(permission string) gorbital.RouteOption {
	return addGuard(route.Guard{
		Name:     "scope:" + permission,
		Statuses: []int{http.StatusForbidden, http.StatusNotFound},
		Scope:    &route.Scope{Permission: permission},
		Err:      validName("permission", permission),
	})
}

// OrgMember refuses callers who aren't members of the organisation in the
// route's {orgId} path parameter with a role granting permission, for
// routes under /v1/orgs/{orgId}/ (ADR-0023, ADR-0048).
//
// Deprecated: organisations are one scope (ADR-0088). Use [Scope], which
// is this guard under the app's own tenancy; an app that mounts
// gorbital.dev/gorbital/orgshttp and changes nothing sees no difference.
// This name keeps working for all of v0.x. Its guard name in logs and
// metrics stays "org_member:<permission>".
func OrgMember(permission string) gorbital.RouteOption {
	return addGuard(route.Guard{
		Name:     "org_member:" + permission,
		Statuses: []int{http.StatusForbidden, http.StatusNotFound},
		Scope:    &route.Scope{Permission: permission},
		Err:      validName("permission", permission),
	})
}

// A RateLimitOption configures [RateLimit].
type RateLimitOption func(*rateLimit)

type rateLimit struct {
	name string
	key  func(ctx context.Context, hctx huma.Context) string
	keys string // what a key is, for /ops/auth/rate-limits
}

// ByUser counts requests per authenticated user or service account, and per
// client address for requests without one (on a public route). It is the
// default.
func ByUser() RateLimitOption {
	return func(l *rateLimit) { l.key, l.keys = byUser, keysByUser }
}

// ByAPIKey counts requests per API key, so each of a user's keys has its
// own budget; requests with a session are counted per user.
func ByAPIKey() RateLimitOption {
	return func(l *rateLimit) {
		l.keys = "API key ID, or actor kind and ID for requests without a key, or client address"
		l.key = func(ctx context.Context, hctx huma.Context) string {
			if p, ok := auth.PrincipalFrom(ctx); ok && p.APIKeyID != "" {
				return "key:" + p.APIKeyID
			}
			return byUser(ctx, hctx)
		}
	}
}

// ByIP counts requests per client address: the address after
// httpx.TrustedProxies, with IPv6 clients grouped by /64 (ratelimit.ClientKey).
func ByIP() RateLimitOption {
	return func(l *rateLimit) { l.key, l.keys = byIP, "client address" }
}

// Named sets the limiter's name. Routes whose limits share a name share
// their budget, so a group can limit all its writes together; they must use
// the same limit. Without it, each route has its own limiter named after its
// operation ID. Names appear in the shared rate-limit store and in
// /ops/auth/rate-limits.
func Named(name string) RateLimitOption {
	return func(l *rateLimit) { l.name = name }
}

// RateLimit allows n requests per window for each caller (see [ByUser],
// [ByAPIKey], [ByIP]) and refuses the rest with 429 rate_limited and a
// Retry-After header. Bursts of up to n requests are allowed.
//
// With gorbital.Deps.RateLimits set, the budget is shared by every instance;
// without it, each instance counts on its own. A limiter that can't decide
// allows the request.
func RateLimit(n int, window time.Duration, opts ...RateLimitOption) gorbital.RouteOption {
	l := rateLimit{key: byUser, keys: keysByUser}
	for _, o := range opts {
		o(&l)
	}
	g := route.Guard{
		Name:     fmt.Sprintf("rate_limit:%d/%s", n, window),
		Statuses: []int{http.StatusTooManyRequests},
		Limit:    &route.Limit{Name: l.name, Limit: ratelimit.Per(n, window), Key: l.key, Keys: l.keys},
	}
	switch {
	case n < 1 || window <= 0:
		g.Err = fmt.Errorf("guard.RateLimit(%d, %s): n and window must be positive", n, window)
	case l.name != "":
		g.Err = validName("rate limiter", l.name)
	}
	return addGuard(g)
}

// keysByUser describes the keys of [ByUser].
const keysByUser = "actor kind and ID, or client address for requests without an actor"

func byUser(ctx context.Context, hctx huma.Context) string {
	if a, ok := actor.From(ctx); ok && a.Kind != actor.KindAnonymous {
		return string(a.Kind) + ":" + a.ID
	}
	return byIP(ctx, hctx)
}

func byIP(_ context.Context, hctx huma.Context) string {
	r, _ := humago.Unwrap(hctx)
	return "ip:" + ratelimit.ByRemoteIP(r)
}

// A Request is what a custom guard can read about the request before its
// input is parsed.
type Request struct {
	hctx huma.Context
}

// PathParam returns the value of a path parameter, such as "id" in
// /v1/books/{id}.
func (r Request) PathParam(name string) string { return r.hctx.Param(name) }

// Header returns the first value of a request header.
func (r Request) Header(name string) string { return r.hctx.Header(name) }

// Query returns the first value of a query parameter.
func (r Request) Query(name string) string { return r.hctx.Query(name) }

// Operation returns the route's OpenAPI operation, such as its ID and path.
func (r Request) Operation() *huma.Operation { return r.hctx.Operation() }

// A Spec describes a custom guard for [New].
type Spec struct {
	// Name identifies the guard in errors, metrics and the OpenAPI
	// document. It is lowercase snake_case, such as "subscription".
	Name string
	// Statuses are the error statuses Check's errors map to, for the
	// OpenAPI document.
	Statuses []int
	// Check returns nil to allow the request. To refuse it, return an error
	// mapped by the module's Errors (or an *httpx.Problem); any other error
	// is a 500, logged once with the request ID.
	Check func(ctx context.Context, req Request) error
}

// New returns a custom guard. Its errors are mapped like the module's other
// errors, so declare them in gorbital.Module.Errors:
//
//	var ErrSubscriptionRequired = errors.New("books: subscription required")
//
//	subscribed := guard.New(guard.Spec{
//		Name:     "subscription",
//		Statuses: []int{http.StatusPaymentRequired},
//		Check: func(ctx context.Context, req guard.Request) error {
//			a, _ := actor.From(ctx)
//			if !plans.Active(ctx, a.ID) {
//				return ErrSubscriptionRequired
//			}
//			return nil
//		},
//	})
func New(spec Spec) gorbital.RouteOption {
	g := route.Guard{Name: spec.Name, Statuses: slices.Clone(spec.Statuses)}
	if spec.Check != nil {
		check := spec.Check
		g.Check = func(hctx huma.Context) error { return check(hctx.Context(), Request{hctx: hctx}) }
	}
	if spec.Check == nil {
		g.Err = fmt.Errorf("guard %q has no Check", spec.Name)
	} else {
		g.Err = validName("guard", spec.Name)
	}
	return addGuard(g)
}

func addGuard(g route.Guard) gorbital.RouteOption {
	return func(c *route.Config) { c.Guards = append(c.Guards, g) }
}

var (
	snakeName      = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	permissionName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

func validName(kind, name string) error {
	pattern := snakeName
	if kind == "permission" {
		pattern = permissionName
	}
	if !pattern.MatchString(name) {
		return fmt.Errorf("%s name %q is invalid", kind, name)
	}
	return nil
}
