package authhttp_test

import (
	"context"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
)

// migrationFiles stands in for an app's db/migrations package.
var migrationFiles embed.FS

// modulesAll stands in for the generated internal/modules.All.
func modulesAll() []gorbital.Module { return nil }

func ExampleNew() {
	// cmd/api/main.go of an app with sign-in: registration, sessions,
	// two-factor authentication, passkeys, Google, Apple and GitHub sign-in
	// and API keys, turned on by their environment variables.
	main := func() {
		gorbital.Main(
			gorbital.WithAuth(authhttp.New()),
			gorbital.WithModules(modulesAll()...),
			gorbital.WithMigrations(migrationFiles),
		)
	}
	_ = main
}

func ExampleAuthenticator() {
	// Without gorbital.Main: build the app from a loaded configuration. New
	// checks the sign-in configuration, then hands the authenticator its
	// dependencies (Setup) before serving.
	ctx := context.Background()
	cfg, err := gorbital.LoadConfig(config.OS)
	if err != nil {
		log.Fatal(err)
	}
	app, err := gorbital.New(ctx, cfg, gorbital.WithAuth(authhttp.New()), gorbital.WithModules(modulesAll()...))
	if err != nil {
		log.Fatal(err)
	}
	log.Fatal(app.Run(ctx))
}

func ExampleAuthenticator_Middleware() {
	// gorbital.New puts the middleware at the Auth step of the stack; a
	// custom order keeps it before the steps that read the caller.
	gorbital.WithStack(func(s gorbital.Stack) []func(http.Handler) http.Handler {
		steps := s.Default()
		// ... reorder or add steps; s.Auth is authhttp's Middleware.
		return steps
	})
	_ = slog.Default()
}

func ExampleAuthenticator_CheckConfig() {
	cfg, err := gorbital.LoadConfig(config.Source{Getenv: func(k string) string {
		return map[string]string{
			"APP_ENV": "production", "STORAGE_DRIVER": "s3", "STORAGE_REGION": "eu-west-1", "STORAGE_BUCKET": "b",
			"STORAGE_ACCESS_KEY": "a", "STORAGE_SECRET_KEY": "s",
			"WEBAUTHN_RP_ID": "example.com", "WEBAUTHN_ORIGINS": "https://example.org",
		}[k]
	}})
	if err != nil {
		log.Fatal(err)
	}
	// gorbital.LoadConfig accepted it; sign-in's own checks don't.
	for line := range strings.Lines(authhttp.New().CheckConfig(cfg).Error()) {
		fmt.Print(line)
	}
	// Output:
	// AUTH_ENCRYPTION_KEYS is required in production: it encrypts two-factor authentication secrets (generate one: echo "k1:$(openssl rand -base64 32)")
	// WEBAUTHN_RP_ID and WEBAUTHN_ORIGINS: passkey: invalid configuration: origin "https://example.org" isn't on the relying party ID "example.com" or a subdomain of it
}

func ExampleAuthenticator_Commands() {
	for _, c := range authhttp.New().Commands() {
		fmt.Println(c.Usage)
	}
	// Output:
	// roles                          list the platform roles and their permissions
	// grant-role <email> <role>      give an account a platform role
	// revoke-role <email> <role>     take a platform role away
	// reset-mfa <email>              turn off an account's two-factor authentication
	// rotate-auth-keys               re-encrypt 2FA secrets with the first AUTH_ENCRYPTION_KEYS key
	// auth-providers                 show which sign-in methods are configured
}

func ExampleAuthenticator_Module() {
	m := authhttp.New().Module()
	fmt.Println(m.Name)
	for _, p := range m.Permissions {
		fmt.Println(p.Name, p.Roles)
	}
	fmt.Println(m.Migrations[0].Version, m.Migrations[len(m.Migrations)-1].Version)
	// Output:
	// auth
	// ops.auth.read [platform_admin ops_viewer]
	// ops.auth.write [platform_admin]
	// ops.service_accounts.read [platform_admin ops_viewer]
	// ops.service_accounts.write [platform_admin]
	// 20260915000001 20260918000070
}

func ExampleAuthenticator_Setup() {
	// gorbital.New and gorbital.Main call Setup; an app never does. A
	// wrapper that adds to sign-in passes the call on.
	type auditedAuth struct{ *authhttp.Authenticator }
	setup := func(ctx context.Context, a auditedAuth, s gorbital.AuthSetup) error {
		s.Deps.Logger.InfoContext(ctx, "sign-in starting", "app", s.Name, "dev_console", s.DevConsole)
		return a.Setup(ctx, s)
	}
	_ = setup
}
