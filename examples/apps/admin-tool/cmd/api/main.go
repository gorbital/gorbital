// Command api runs the admin tool, an internal back-office API where staff
// publish the announcements customers see, built on gorbital.Main.
//
//	go run ./cmd/api                    serve the API (APP_ENV and DATABASE_URL from the environment)
//	go run ./cmd/api migrate            apply migrations
//	go run ./cmd/api openapi --dir api  write the OpenAPI document
//	go run ./cmd/api help               list every command
package main

import (
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/opshttp"

	"example.com/admin-tool/db/migrations"
	"example.com/admin-tool/internal/modules"
)

// docs:start main
func main() {
	gorbital.Main(
		gorbital.WithName("admin-tool"),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module()), // /ops/ and /v1/flags, built in
		gorbital.WithModules(modules.All()...),                     // internal/modules/modules.gen.go
		gorbital.WithMigrations(migrations.FS),                     // db/migrations
	)
}

// docs:end main
