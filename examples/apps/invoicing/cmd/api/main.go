// Command api runs the invoicing API: companies are organisations, and
// their members keep the company's invoices, which row-level security
// separates in the database too. Built on gorbital.Main.
//
//	go run ./cmd/api                    serve the API (APP_ENV and DATABASE_URL from the environment)
//	go run ./cmd/api migrate            apply migrations
//	go run ./cmd/api openapi --dir api  write the OpenAPI document
//	go run ./cmd/api help               list every command
package main

import (
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/gorbital/orgshttp"

	"example.com/invoicing/db/migrations"
	"example.com/invoicing/internal/modules"
)

// docs:start main
func main() {
	auth := authhttp.New() // sign-in: accounts, sessions, MFA, passkeys, API keys
	gorbital.Main(
		gorbital.WithName("invoicing"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(opshttp.Module(), orgshttp.Module(auth)), // /ops/, and companies: organisations, members, invitations
		gorbital.WithModules(modules.All()...),                        // internal/modules/modules.gen.go: invoices
		gorbital.WithMigrations(migrations.FS),                        // db/migrations: row-level security, then invoices
	)
}

// docs:end main
