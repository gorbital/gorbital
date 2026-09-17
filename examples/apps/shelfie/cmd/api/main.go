// Command api runs Shelfie, a reading-tracker API for a web and a mobile
// app, built on gorbital.Main.
//
//	go run ./cmd/api                    serve the API (APP_ENV and DATABASE_URL from the environment)
//	go run ./cmd/api migrate            apply migrations
//	go run ./cmd/api openapi --dir api  write the OpenAPI document
//	go run ./cmd/api grant-role <email> <role>
//	                                    give an account a platform role
//	go run ./cmd/api help               list every command
package main

import (
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/gorbital/orgshttp"

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules"
	"example.com/shelfie/internal/modules/partners"
	"example.com/shelfie/internal/modules/phonelogin"
)

// docs:start main
func main() {
	auth := authhttp.New(signInOptions()...) // sign-in: accounts, sessions, MFA, passkeys, API keys; signin.go
	gorbital.Main(
		gorbital.WithName("shelfie"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module()),         // /ops/ and /v1/flags, built in
		gorbital.WithModules(modules.All()...),                             // internal/modules/modules.gen.go
		gorbital.WithModules(phonelogin.Module(auth, smsSender())),         // takes the authenticator, so it's added here
		gorbital.WithModules(partners.Module(partnerName, partnerSecrets)), // the bookshop's signed purchase webhooks; partners.go
		gorbital.WithModules(orgshttp.Module(auth)),                        // book clubs: organisations, members and invitations
		gorbital.WithMigrations(migrations.FS),                             // db/migrations
	)
}

// docs:end main
