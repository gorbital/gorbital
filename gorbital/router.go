package gorbital

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/metric"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital/internal/route"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/ratelimitpg"
)

// A RouteOption configures a route, or every route of a group. Options of a
// group apply first, then the route's own.
type RouteOption = route.Option

// A Router registers a module's routes under a path prefix with shared
// options. [Mount] passes each module's Routes a Router for that module.
type Router struct {
	reg    *registry
	module string
	prefix string
	opts   []RouteOption
}

// Group returns a Router for the routes under prefix, which is empty or
// starts with a slash, with opts added to the options of r.
func (r *Router) Group(prefix string, opts ...RouteOption) *Router {
	full, err := joinPath(r.prefix, prefix)
	if err != nil {
		r.reg.fail(fmt.Errorf("gorbital: module %q: group %q: %w", r.module, prefix, err))
	}
	return &Router{reg: r.reg, module: r.module, prefix: full, opts: append(slices.Clip(r.opts), opts...)}
}

// Get registers a GET operation for path under r's prefix. The handler takes
// the request context and its typed input, and returns its typed output or an
// error; Huma validates the input and documents both (ADR-0082).
func Get[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption) {
	register(r, http.MethodGet, path, handler, opts)
}

// Post registers a POST operation. See [Get].
func Post[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption) {
	register(r, http.MethodPost, path, handler, opts)
}

// Put registers a PUT operation. See [Get].
func Put[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption) {
	register(r, http.MethodPut, path, handler, opts)
}

// Patch registers a PATCH operation. See [Get].
func Patch[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption) {
	register(r, http.MethodPatch, path, handler, opts)
}

// Delete registers a DELETE operation. See [Get].
func Delete[I, O any](r *Router, path string, handler func(context.Context, *I) (*O, error), opts ...RouteOption) {
	register(r, http.MethodDelete, path, handler, opts)
}

// Tags sets the OpenAPI tags. Without it, a route has none.
func Tags(tags ...string) RouteOption {
	return func(c *route.Config) { c.Tags = slices.Clone(tags) }
}

// Summary sets the OpenAPI summary. Without it, Huma generates one from the
// method and path.
func Summary(s string) RouteOption { return func(c *route.Config) { c.Summary = s } }

// Description sets the OpenAPI description, in Markdown.
func Description(markdown string) RouteOption {
	return func(c *route.Config) { c.Description = markdown }
}

// OperationID sets the operation ID. Without it, the ID is the module's name
// followed by the one Huma generates from the method and path, such as
// "books-post-v1-books". Operation IDs are public API: client generators
// name their functions after them.
func OperationID(id string) RouteOption { return func(c *route.Config) { c.OperationID = id } }

// Status sets the success status, such as http.StatusCreated.
func Status(code int) RouteOption { return func(c *route.Config) { c.Status = code } }

// Errors documents error statuses the route returns, in addition to those
// its guards document.
func Errors(statuses ...int) RouteOption {
	return func(c *route.Config) { c.Errors = append(c.Errors, statuses...) }
}

// Deprecated marks the route deprecated in the OpenAPI document.
func Deprecated() RouteOption { return func(c *route.Config) { c.Deprecated = true } }

// errUnauthenticated is the response of the check every non-public route
// runs; the code is the one v0.1 endpoints return.
var errUnauthenticated = httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "authentication is required")

// requireActor refuses a request that has no authenticated actor before its
// input is parsed (ADR-0082).
func requireActor(api huma.API) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if a, ok := actor.From(ctx.Context()); ok && a.Kind != actor.KindAnonymous {
			next(ctx)
			return
		}
		_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, errUnauthenticated.Detail, errUnauthenticated)
	}
}

func register[I, O any](r *Router, method, path string, handler func(context.Context, *I) (*O, error), opts []RouteOption) {
	reg := r.reg
	if reg.err != nil {
		return
	}
	full, err := joinPath(r.prefix, path)
	if err == nil && full == "" {
		err = errors.New("path is empty")
	}
	if err == nil && handler == nil {
		err = errNilHandler
	}
	if err != nil {
		reg.fail(fmt.Errorf("gorbital: module %q: %s %q: %w", r.module, method, r.prefix+path, err))
		return
	}

	var cfg route.Config
	for _, o := range r.opts {
		o(&cfg)
	}
	for _, o := range opts {
		o(&cfg)
	}

	var out *O
	op := huma.Operation{
		OperationID:   cfg.OperationID,
		Method:        method,
		Path:          full,
		Summary:       cfg.Summary,
		Description:   cfg.Description,
		Tags:          cfg.Tags,
		DefaultStatus: cfg.Status,
		Errors:        slices.Clone(cfg.Errors),
		Deprecated:    cfg.Deprecated,
	}
	if op.OperationID == "" {
		op.OperationID = r.module + "-" + huma.GenerateOperationID(method, full, out)
	}
	if op.Summary == "" {
		op.Summary = huma.GenerateSummary(method, full, out)
	}
	// Operation middleware order (ADR-0082): middleware, the sign-in check,
	// then guards in the order declared; all before input parsing.
	if len(cfg.Middlewares) > 0 {
		op.Middlewares = append(op.Middlewares, adapt(cfg.Middlewares))
	}
	guards := []string{"public"}
	if !cfg.Public {
		if !reg.hasBearer() {
			reg.fail(fmt.Errorf("gorbital: module %q: %s %s requires authentication, but the API declares no bearer security scheme; create it with openapi.WithBearerAuth, or mark the route guard.Public()", r.module, method, full))
			return
		}
		op.Security = openapi.Bearer
		op.Errors = append([]int{http.StatusUnauthorized}, op.Errors...)
		op.Middlewares = append(op.Middlewares, requireActor(reg.api))
		guards[0] = "authenticated"
	}
	for _, g := range cfg.Guards {
		if g.Err != nil {
			reg.fail(fmt.Errorf("gorbital: module %q: %s %s: %w", r.module, method, full, g.Err))
			return
		}
		mw, err := reg.guardMiddleware(r.module, &op, g)
		if err != nil {
			reg.fail(fmt.Errorf("gorbital: module %q: %s %s: %w", r.module, method, full, err))
			return
		}
		op.Middlewares = append(op.Middlewares, mw)
		op.Errors = append(op.Errors, g.Statuses...)
		guards = append(guards, g.Name)
	}
	slices.Sort(op.Errors)
	op.Errors = slices.Compact(op.Errors)
	op.Extensions = map[string]any{"x-gorbital-guards": guards}

	if err := reg.claim(r.module, op); err != nil {
		reg.fail(err)
		return
	}
	what := fmt.Sprintf("%s %s", method, full)
	if err := catchPanic(r.module, what, func() { huma.Register(reg.api, op, handler) }); err != nil {
		reg.fail(err)
	}
}

// registry holds what [Mount] has registered so far, across modules.
type registry struct {
	api        huma.API
	mapper     *httpx.Mapper
	rateLimits *ratelimitpg.Store
	err        error
	ops        map[string]string // operation ID → module
	paths      map[string]string // method and path with parameters blanked → module
	limiters   map[string]sharedLimiter
	refusals   metric.Int64Counter
}

func newRegistry(api huma.API, mapper *httpx.Mapper, rateLimits *ratelimitpg.Store) *registry {
	return &registry{
		api: api, mapper: mapper, rateLimits: rateLimits,
		ops: map[string]string{}, paths: map[string]string{}, limiters: map[string]sharedLimiter{},
		refusals: newRefusals(),
	}
}

// fail keeps the first registration error; later registrations are skipped.
func (g *registry) fail(err error) {
	if g.err == nil {
		g.err = err
	}
}

func (g *registry) hasBearer() bool {
	c := g.api.OpenAPI().Components
	return c != nil && c.SecuritySchemes[openapi.BearerScheme] != nil
}

var pathParam = regexp.MustCompile(`\{[^}]*\}`)

// claim records op's ID and route, refusing ones another route has.
func (g *registry) claim(module string, op huma.Operation) error {
	if prev, ok := g.ops[op.OperationID]; ok {
		return fmt.Errorf("gorbital: operation ID %q is used by modules %q and %q", op.OperationID, prev, module)
	}
	key := op.Method + " " + pathParam.ReplaceAllString(op.Path, "{}")
	if prev, ok := g.paths[key]; ok {
		return fmt.Errorf("gorbital: %s %s is registered by modules %q and %q", op.Method, op.Path, prev, module)
	}
	g.ops[op.OperationID] = module
	g.paths[key] = module
	return nil
}

// joinPath joins a group prefix and a path. Each is empty or starts with a
// slash and doesn't end with one; the result follows the same rule.
func joinPath(prefix, path string) (string, error) {
	for _, p := range []string{prefix, path} {
		switch {
		case p == "":
		case !strings.HasPrefix(p, "/"):
			return "", fmt.Errorf("%q must start with a slash", p)
		case strings.HasSuffix(p, "/"):
			return "", fmt.Errorf("%q must not end with a slash", p)
		case strings.Contains(p, "//"):
			return "", fmt.Errorf("%q has an empty segment", p)
		}
	}
	return prefix + path, nil
}

// moduleLogger tags logger with the module's name, or discards when nil.
func moduleLogger(logger *slog.Logger, module string) *slog.Logger {
	if logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return logger.With("module", module)
}
