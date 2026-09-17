package gorbital

import (
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/httpx"
)

// Mount adds each module's error mappings to mapper and registers its routes
// on api, in module order. The app calls it once, after [Declare] and after
// building the stores that deps hold; to export the OpenAPI document without
// a database, pass zero Deps.
//
// api must declare the bearer security scheme (openapi.WithBearerAuth) when
// any route requires authentication, and errors are written as problem+json
// only when the app has called openapi.InstallErrors with mapper.
//
// Mount returns the first error, naming the module: an invalid or duplicate
// module name, errors to map without a mapper, a mapping the mapper refuses,
// an invalid path, two routes with the same method and path or the same
// operation ID, or a route Huma can't register (such as an unsupported input
// type). Routes registered before the error stay registered, so an app
// treats any error as fatal.
func Mount(api huma.API, mapper *httpx.Mapper, deps Deps, modules ...Module) error {
	_, err := mount(api, mapper, deps, modules)
	return err
}

// mount is Mount, returning what was registered, such as the rate limiters
// guards use.
func mount(api huma.API, mapper *httpx.Mapper, deps Deps, modules []Module) (*registry, error) {
	if err := validateModules(modules); err != nil {
		return nil, err
	}
	reg := newRegistry(api, mapper, deps.RateLimits)
	for _, m := range modules {
		if len(m.Errors) > 0 {
			if mapper == nil {
				return nil, fmt.Errorf("gorbital: module %q maps errors, but the mapper is nil", m.Name)
			}
			if err := mapper.Add(m.Errors...); err != nil {
				return nil, fmt.Errorf("gorbital: module %q: %w", m.Name, err)
			}
		}
		if m.Routes == nil {
			continue
		}
		d := deps
		d.Logger = moduleLogger(deps.Logger, m.Name)
		r := &Router{reg: reg, module: m.Name}
		if len(m.Middleware) > 0 {
			r.opts = []RouteOption{Use(m.Middleware...)}
		}
		if err := catchPanic(m.Name, "routes", func() { m.Routes(r, d) }); err != nil {
			reg.fail(err)
		}
		if reg.err != nil {
			return nil, reg.err
		}
	}
	return reg, nil
}
