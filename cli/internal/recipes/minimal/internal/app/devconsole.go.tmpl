package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"gorbital.dev/buildinfo"
	"gorbital.dev/config"
	"gorbital.dev/modules/devconsole"
)

// The development console's APIs under /_dev/ (ADR-0065): what the app
// wired, its routes, the environment variables it read, recent requests and
// logs. They exist only when APP_ENV is development and DEV_CONSOLE_TOKEN
// is set, which orb dev does with a new random token on every run; every
// request needs a localhost Host header, a local connection and that token.
// See docs/guides/dev-console.md.

// loadDevConsoleToken reads DEV_CONSOLE_TOKEN: refused in production,
// checked for length in development.
func loadDevConsoleToken(secret func(key string) config.Secret, production bool) (config.Secret, error) {
	token := secret("DEV_CONSOLE_TOKEN")
	switch {
	case token.IsZero():
		return token, nil
	case production:
		return config.Secret{}, errors.New("DEV_CONSOLE_TOKEN is for local development only: unset it in production")
	case devconsole.CheckToken(token.Reveal()) != nil:
		return config.Secret{}, fmt.Errorf("DEV_CONSOLE_TOKEN must be %d to %d visible ASCII characters; orb dev generates one",
			devconsole.MinTokenLength, devconsole.MaxTokenLength)
	}
	return token, nil
}

// devConsoleOn reports whether the app serves the dev console.
func (c Config) devConsoleOn() bool { return c.Env == "development" && !c.DevConsoleToken.IsZero() }

// newDevConsoleLogs returns the buffer of recent log records the console
// serves, or nil when the console is off.
func newDevConsoleLogs(cfg Config) (*devconsole.Logs, error) {
	if !cfg.devConsoleOn() {
		return nil, nil // no console, no buffer
	}
	return devconsole.NewLogs(devconsole.DefaultMaxLogs)
}

// buildDevConsole creates the console when it is on; a.console stays nil
// otherwise, which serves nothing.
func (a *App) buildDevConsole(logs *devconsole.Logs) error {
	if !a.cfg.devConsoleOn() {
		return nil
	}
	console, err := devconsole.New(a.cfg.DevConsoleToken.Reveal(),
		devconsole.WithAddr(a.cfg.Addr),
		devconsole.WithLogs(logs),
		devconsole.WithSources(devconsole.Sources{
			App:    a.devApp,
			Routes: a.devRoutes,
			Config: func(context.Context) ([]devconsole.EnvKey, error) { return a.cfg.EnvKeys, nil },
		}),
	)
	if err != nil {
		return err
	}
	a.console = console
	return nil
}

// devApp describes the app for GET /_dev/app.
func (a *App) devApp(ctx context.Context) (devconsole.App, error) {
	routes, err := a.devRoutes(ctx)
	if err != nil {
		return devconsole.App{}, err
	}
	info := buildinfo.Read()
	return devconsole.App{
		Name: ServiceName, Version: info.Version, Commit: info.Commit, GoVersion: info.GoVersion, Env: a.cfg.Env,
		Libraries:   devconsole.Libraries(),
		Modules:     routeTags(routes),
		Jobs:        []devconsole.Job{},
		Settings:    []devconsole.Setting{},
		Flags:       []devconsole.Flag{},
		Permissions: []devconsole.PermissionCatalog{},
	}, nil
}

// devRoutes lists the OpenAPI operations and the plain handlers
// buildHTTP mounts (routes.go), for GET /_dev/routes.
func (a *App) devRoutes(context.Context) ([]devconsole.Route, error) {
	doc, err := json.Marshal(a.api.OpenAPI())
	if err != nil {
		return nil, err
	}
	routes, err := devconsole.RoutesFromOpenAPI(doc)
	if err != nil {
		return nil, err
	}
	routes = append(routes, a.handlerRoutes()...)
	devconsole.SortRoutes(routes)
	return routes, nil
}

// handlerRoutes are the routes buildHTTP mounts outside the OpenAPI
// document. Keep them in step with routes.go; TestDevConsoleRoutes checks
// each is served.
func (a *App) handlerRoutes() []devconsole.Route {
	paths := []string{"/livez", "/readyz"}
	if a.cfg.DocsEnabled {
		paths = append(paths, "/docs", "/openapi.json", "/openapi.yaml")
	}
	routes := make([]devconsole.Route, 0, len(paths))
	for _, p := range paths {
		routes = append(routes, devconsole.Route{Method: "GET", Path: p, Tags: []string{}, Source: devconsole.RouteHandler})
	}
	return routes
}

// routeTags returns the tags of routes, sorted and without duplicates.
func routeTags(routes []devconsole.Route) []string {
	tags := []string{}
	for _, r := range routes {
		tags = append(tags, r.Tags...)
	}
	slices.Sort(tags)
	return slices.Compact(tags)
}
