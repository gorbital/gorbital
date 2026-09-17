// Command api runs the billing API, which records the payment provider's
// webhooks, built on gorbital.Main.
//
//	go run ./cmd/api                    serve the API (APP_ENV, DATABASE_URL and PAYMENTS_WEBHOOK_SECRET from the environment)
//	go run ./cmd/api migrate            apply migrations
//	go run ./cmd/api openapi --dir api  write the OpenAPI document
//	go run ./cmd/api help               list every command
package main

import (
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/opshttp"

	"example.com/payments/db/migrations"
	"example.com/payments/internal/modules"
)

// docs:start main
func main() {
	gorbital.Main(
		gorbital.WithName("payments"),
		gorbital.WithModules(opshttp.Module()), // /ops/, where the receipt job is operated
		gorbital.WithModules(modules.All()...), // internal/modules/modules.gen.go
		gorbital.WithMigrations(migrations.FS), // db/migrations
	)
}

// docs:end main
