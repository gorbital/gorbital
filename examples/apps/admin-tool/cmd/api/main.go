// Command api runs the admin tool, an internal back-office API where staff
// publish the announcements customers see, built on gorbital.Main.
//
//	go run ./cmd/api                    serve the API (APP_ENV and DATABASE_URL from the environment)
//	go run ./cmd/api migrate            apply migrations
//	go run ./cmd/api openapi --dir api  write the OpenAPI document
//	go run ./cmd/api grant-role <email> <role>
//	                                    give an account a platform role
//	go run ./cmd/api help               list every command
package main

import (
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/opshttp"

	"example.com/admin-tool/db/migrations"
	"example.com/admin-tool/internal/modules"
	"example.com/admin-tool/internal/modules/announcements"
)

// docs:start main
func main() {
	auth := authhttp.New(signInOptions()...) // sign-in: accounts, sessions, second factors, API keys
	gorbital.Main(
		gorbital.WithName("admin-tool"),
		gorbital.WithAuth(auth),                                    // /v1/auth/, the platform roles and their second factor
		gorbital.WithModules(opshttp.Module(), flagshttp.Module()), // /ops/ and /v1/flags, built in
		gorbital.WithModules(modules.All()...),                     // internal/modules/modules.gen.go
		gorbital.WithMigrations(migrations.FS),                     // db/migrations
	)
}

// docs:end main

// docs:start sign-in-options

// signInOptions is how an internal tool differs from a product: nobody
// signs up, passwords are longer than the library's minimum, API keys
// expire within the month, and the role that changes what customers see
// needs a second factor. Everything else is v0.1's sign-in.
func signInOptions() []authhttp.Option {
	return []authhttp.Option{
		authhttp.WithoutRegistration(),                // accounts come from POST /ops/auth/users, never sign-up
		authhttp.MinPasswordLength(16),                // staff keep them in the company password manager
		authhttp.APIKeyMaxTTL(30 * 24 * time.Hour),    // keys belong to scripts, renewed monthly
		authhttp.RequireMFA(announcements.RoleEditor), // as sign-in already does for platform_admin and ops_viewer
	}
}

// docs:end sign-in-options
