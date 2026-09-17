package gorbital

import (
	"context"
	"fmt"
	"net/http"

	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/settings"
)

// AuthSetup is what the app hands its authenticator before using it: the
// configuration, the dependencies and the permission catalog. An
// [Authenticator] with a method
//
//	Setup(ctx context.Context, s gorbital.AuthSetup) error
//
// receives it, explicitly and once per app:
//
//   - from [New], after the stores exist and before the modules' jobs,
//     routes and the middleware stack are built, with the app's Deps; an
//     error fails New;
//   - from [Main], before a command the authenticator contributes runs, with
//     zero Deps: the command opens what it needs from Config.
//
// An authenticator that also has a method CheckConfig(cfg Config) error has
// it called first, by New before connecting to anything and by Main before
// such a command; its error is a configuration error (exit status 2), as
// for [LoadConfig]. gorbital.dev/gorbital/authhttp checks there what needs
// sign-in's own packages, such as the Apple private key.
type AuthSetup struct {
	// Name is the app's name ([WithName]).
	Name string
	// Config is the loaded configuration.
	Config Config
	// Deps are the app's dependencies, with an untagged logger; zero from
	// Main.
	Deps Deps
	// Permissions is the app's permission catalog: every module's
	// permissions, and a role for every role name they use, with the
	// permissions the modules grant it. It isn't frozen yet, so the
	// authenticator can declare the roles it relies on and require a second
	// factor for roles; the authenticator freezes it.
	Permissions *auth.Catalog
	// DevConsole reports whether the app serves the development console
	// (APP_ENV=development with DEV_CONSOLE_TOKEN): features that must never
	// run in production, such as impersonation, check it.
	DevConsole bool
	// Handle serves a handler outside the OpenAPI document on the app's mux,
	// behind the middleware stack, such as the /.well-known files passkeys
	// need. The pattern is an http.ServeMux pattern with a method, such as
	// "GET /.well-known/assetlinks.json". The dev console lists it. Call it
	// during Setup: New mounts the handlers once Setup returns, and fails
	// for a pattern that conflicts with another route. It does nothing from
	// Main.
	Handle func(pattern string, handler http.Handler)
	// MailPreviews adds emails the dev console previews and sends with
	// sample data (ADR-0074). Call it during Setup. It does nothing from
	// Main.
	MailPreviews func(previews ...auth.EmailPreview)
}

// authSetupper is the optional method of an authenticator that receives
// its AuthSetup.
type authSetupper interface {
	Setup(ctx context.Context, s AuthSetup) error
}

// configChecker is the optional method of an authenticator that checks the
// configuration it reads.
type configChecker interface {
	CheckConfig(cfg Config) error
}

// checkAuthConfig runs the authenticator's configuration check, if it has
// one, as a configuration error.
func (o options) checkAuthConfig(cfg Config) error {
	c, ok := o.auth.(configChecker)
	if !ok {
		return nil
	}
	if err := c.CheckConfig(cfg); err != nil {
		return fmt.Errorf("%w:\n%w", errInvalidConfig, err)
	}
	return nil
}

// permissionCatalog declares every module's permissions in a new catalog,
// with the roles they grant, as New does, without building any store.
func permissionCatalog(modules []Module) (*auth.Catalog, error) {
	catalog := auth.NewCatalog()
	d := Declarations{Permissions: catalog, Settings: settings.NewRegistry(), Flags: flags.NewRegistry()}
	if err := Declare(d, modules...); err != nil {
		return nil, err
	}
	if err := declareRoles(catalog, modules); err != nil {
		return nil, err
	}
	return catalog, nil
}

// setupAuthForCommand gives the authenticator the configuration and the
// catalog before one of its commands runs.
func (o options) setupAuthForCommand(ctx context.Context, cfg Config) error {
	if err := o.checkAuthConfig(cfg); err != nil {
		return err
	}
	s, ok := o.auth.(authSetupper)
	if !ok {
		return nil
	}
	modules := o.allModules()
	if err := validateModules(modules); err != nil {
		return err
	}
	catalog, err := permissionCatalog(modules)
	if err != nil {
		return err
	}
	return s.Setup(ctx, AuthSetup{
		Name: o.name, Config: cfg, Permissions: catalog, DevConsole: cfg.devConsoleOn(),
		Handle:       func(string, http.Handler) {},
		MailPreviews: func(...auth.EmailPreview) {},
	})
}

// handledRoute is a handler the authenticator serves outside the OpenAPI
// document.
type handledRoute struct {
	pattern string
	handler http.Handler
}

// setupAuth gives the authenticator its AuthSetup, when it takes one.
func (a *App) setupAuth(ctx context.Context) error {
	s, ok := a.o.auth.(authSetupper)
	if !ok {
		return nil
	}
	deps := a.deps
	return s.Setup(ctx, AuthSetup{
		Name: a.o.name, Config: a.cfg, Deps: deps, Permissions: a.catalog, DevConsole: a.cfg.devConsoleOn(),
		Handle: func(pattern string, handler http.Handler) {
			a.handlers = append(a.handlers, handledRoute{pattern: pattern, handler: handler})
		},
		MailPreviews: func(previews ...auth.EmailPreview) { a.mailPreviews = append(a.mailPreviews, previews...) },
	})
}
