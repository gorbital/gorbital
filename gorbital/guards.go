package gorbital

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/gorbital/internal/route"
	"gorbital.dev/httpx"
	"gorbital.dev/httpx/timeout"
	"gorbital.dev/ratelimit"
)

// Use adds middleware to a route, or to every route of a group. Middleware
// runs in the order given, after the module's and the group's middleware and
// before the route's guards and input parsing (ADR-0082). Any standard
// middleware works:
//
//	books := r.Group("/v1/books", gorbital.Use(requireClientVersion("2.4.0")))
//
// A route can't remove middleware its group added: put routes that need
// different middleware in their own group.
//
// Route middleware needs an API built on Huma's humago adapter, as
// openapi.New builds it.
func Use(middlewares ...func(http.Handler) http.Handler) RouteOption {
	return func(c *route.Config) { c.Middlewares = append(c.Middlewares, middlewares...) }
}

// Timeout gives a route a shorter deadline than the app's request timeout
// (APP_REQUEST_TIMEOUT): after d, a handler that hasn't started its
// response gets 503 request_timeout, and its context is cancelled. A
// context deadline can only be shortened, so a longer d has no effect: raise
// APP_REQUEST_TIMEOUT, or leave the Timeout step out with [WithStack], for
// routes that need longer. Streaming responses that have started aren't
// cut off (timeout.New).
func Timeout(d time.Duration) RouteOption {
	return Use(timeout.New(d))
}

// continuation carries Huma's next step through standard middleware.
type continuation struct {
	op   *huma.Operation
	next func(huma.Context)
}

type continuationKey struct{}

// adapt builds the middleware chain once for a route and returns it as a
// Huma operation middleware. Per request it only stores the continuation in
// the request's context, so middleware constructors don't run per request.
func adapt(middlewares []func(http.Handler) http.Handler) func(huma.Context, func(huma.Context)) {
	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := r.Context().Value(continuationKey{}).(*continuation)
		c.next(humago.NewContext(c.op, r, w))
	})
	for _, mw := range slices.Backward(middlewares) {
		h = mw(h)
	}
	return func(hctx huma.Context, next func(huma.Context)) {
		r, w := humago.Unwrap(hctx)
		ctx := context.WithValue(r.Context(), continuationKey{}, &continuation{op: hctx.Operation(), next: next})
		h.ServeHTTP(w, r.WithContext(ctx))
	}
}

// rateLimited is the refusal of a rate-limit guard.
type rateLimited struct{ retryAfter int }

var errRateLimited = httpx.NewProblem(http.StatusTooManyRequests, "rate_limited", "too many requests; retry after the time in Retry-After")

func (e *rateLimited) Error() string { return errRateLimited.Error() }
func (e *rateLimited) Unwrap() error { return errRateLimited }

// limiter returns the shared limiter name for limit, creating it once, and
// records the route that uses it.
func (g *registry) limiter(name string, limit ratelimit.Limit, keys, route string) (ratelimit.Taker, error) {
	if !limit.Valid() {
		return nil, fmt.Errorf("rate limit %q is invalid: %+v", name, limit)
	}
	if l, ok := g.limiters[name]; ok {
		if l.limit != limit {
			return nil, fmt.Errorf("rate limiter %q is used with two limits: %+v and %+v", name, l.limit, limit)
		}
		l.routes = append(l.routes, route)
		g.limiters[name] = l
		return l.taker, nil
	}
	var taker ratelimit.Taker
	if g.rateLimits != nil {
		l, err := g.rateLimits.Limiter(name, func(context.Context) ratelimit.Limit { return limit })
		if err != nil {
			return nil, err
		}
		taker = l
	} else {
		// Without the shared store (no database), each instance counts on
		// its own, as ratelimitpg does when the database can't answer.
		taker = ratelimit.New(limit.PerSecond, limit.Burst)
	}
	g.limiters[name] = sharedLimiter{limit: limit, taker: taker, keys: keys, routes: []string{route}}
	return taker, nil
}

type sharedLimiter struct {
	limit  ratelimit.Limit
	taker  ratelimit.Taker
	keys   string   // what a key is
	routes []string // method and path of each route using it
}

// guardMiddleware returns the operation middleware that runs guard.
func (g *registry) guardMiddleware(module string, op *huma.Operation, guard route.Guard, public bool) (func(huma.Context, func(huma.Context)), error) {
	if guard.Scope != nil {
		return g.scopeMiddleware(module, op, guard, public)
	}
	check := guard.Check
	if guard.Limit != nil {
		name := guard.Limit.Name
		if name == "" {
			name = op.OperationID
		}
		taker, err := g.limiter(name, guard.Limit.Limit, guard.Limit.Keys, op.Method+" "+op.Path)
		if err != nil {
			return nil, err
		}
		key := guard.Limit.Key
		check = func(hctx huma.Context) error {
			d, err := taker.Take(hctx.Context(), key(hctx.Context(), hctx))
			if err != nil || d.Allowed {
				return nil //nolint:nilerr // a limiter that can't decide allows the request (ratelimit.Taker)
			}
			return &rateLimited{retryAfter: int(math.Ceil(d.RetryAfter.Seconds()))}
		}
	}
	if check == nil {
		return nil, fmt.Errorf("guard %q has no check", guard.Name)
	}
	routeAttr := routeAttribute(op.Path)
	return func(hctx huma.Context, next func(huma.Context)) {
		err := check(hctx)
		if err == nil {
			next(hctx)
			return
		}
		g.refuse(hctx, module, guard.Name, routeAttr, err)
	}, nil
}

// routeAttribute is a refusal's route attribute.
func routeAttribute(path string) attribute.KeyValue { return attribute.String("http.route", path) }

// refuse writes a guard's refusal as problem+json and records it.
func (g *registry) refuse(hctx huma.Context, module, guard string, routeAttr attribute.KeyValue, err error) {
	ctx := hctx.Context()
	attrs := metric.WithAttributes(attribute.String("gorbital.guard", guard), attribute.String("gorbital.module", module), routeAttr)
	g.refusals.Add(ctx, 1, attrs)
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("gorbital.guard.refused", guard))

	var limited *rateLimited
	if errors.As(err, &limited) {
		hctx.SetHeader("Retry-After", strconv.Itoa(max(limited.retryAfter, 1)))
	}
	status := http.StatusInternalServerError
	if p, ok := g.match(err); ok {
		status = p.Status
	}
	_ = huma.WriteErr(g.api, hctx, status, http.StatusText(status), err)
}

// match finds the problem for err: a *httpx.Problem, or a mapped error.
func (g *registry) match(err error) (*httpx.Problem, bool) {
	var p *httpx.Problem
	if errors.As(err, &p) {
		return p, true
	}
	if g.mapper != nil {
		return g.mapper.Match(err)
	}
	return nil, false
}

// newRefusals returns the guard refusal counter from the global meter
// provider, which telemetry.Setup configures; without one it is a no-op.
func newRefusals() metric.Int64Counter {
	c, err := otel.Meter("gorbital.dev/gorbital").Int64Counter("gorbital.guard.refusals",
		metric.WithDescription("Requests a route guard refused, by guard, module and route"),
		metric.WithUnit("{request}"))
	if err != nil {
		otel.Handle(err)
		return noop.Int64Counter{}
	}
	return c
}
