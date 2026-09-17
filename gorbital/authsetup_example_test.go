package gorbital_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"gorbital.dev/gorbital"
)

// tokenAuth is an authenticator that reads its configuration and database
// from the app, as gorbital.dev/gorbital/authhttp does.
type tokenAuth struct {
	app  string
	deps gorbital.Deps
}

func (a *tokenAuth) Middleware(*slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Look the bearer token up in a.deps.DB and set the actor.
			next.ServeHTTP(w, r)
		})
	}
}

// CheckConfig runs before anything connects; an error exits with status 2.
func (a *tokenAuth) CheckConfig(cfg gorbital.Config) error {
	if cfg.Production() && cfg.Auth.EncryptionKeys.IsZero() {
		return errors.New("AUTH_ENCRYPTION_KEYS is required in production")
	}
	return nil
}

// Setup receives the app's dependencies once the stores exist.
func (a *tokenAuth) Setup(_ context.Context, s gorbital.AuthSetup) error {
	a.app, a.deps = s.Name, s.Deps
	if !s.Permissions.HasRole("user") {
		s.Permissions.Role("user", "Every signed-in user")
	}
	s.Permissions.Freeze()
	s.Handle("GET /.well-known/token-issuer", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(a.app))
	}))
	return nil
}

func ExampleAuthSetup() {
	main := func() {
		gorbital.Main(gorbital.WithAuth(&tokenAuth{}), gorbital.WithModules(modulesAll()...))
	}
	_ = main
}
