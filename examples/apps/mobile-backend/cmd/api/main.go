// Command api runs the mobile backend, an API whose users sign in with an
// external identity provider, built on gorbital.Main.
//
//	go run ./cmd/api                    serve the API (APP_ENV, DATABASE_URL and IDP_* from the environment)
//	go run ./cmd/api migrate            apply migrations
//	go run ./cmd/api openapi --dir api  write the OpenAPI document
//	go run ./cmd/api help               list every command
package main

import (
	"gorbital.dev/config"
	"gorbital.dev/gorbital"

	"example.com/mobile-backend/db/migrations"
	"example.com/mobile-backend/internal/modules"
)

// docs:start main
func main() {
	gorbital.Main(
		gorbital.WithName("mobile-backend"),
		// The app has no sign-in of its own: the identity provider's tokens
		// are the only way in (idp.go). It reads IDP_* from the environment.
		gorbital.WithAuth(newIdentityProvider(config.OS)),
		gorbital.WithModules(modules.All()...), // internal/modules/modules.gen.go
		gorbital.WithMigrations(migrations.FS), // db/migrations
	)
}

// docs:end main
