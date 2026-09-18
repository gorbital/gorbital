// Command api runs plateful, built on gorbital.Main: the server, and the
// commands its modules add.
//
//	go run ./cmd/api                     serve the API (APP_ENV and DATABASE_URL from the environment)
//	go run ./cmd/api migrate             apply migrations; --status reports pending ones
//	go run ./cmd/api openapi --dir api   write the OpenAPI document, the Postman collection and llms.txt
//	go run ./cmd/api seed                create the development administrator (orb dev runs it)
//	go run ./cmd/api grant-role <email> <role>
//	                                     give an account a platform role
//	go run ./cmd/api help                list every command
package main

import (
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/mailevents"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/gorbital/orgshttp"

	"example.com/plateful/db/migrations"
	"example.com/plateful/internal/modules"
)

func main() {
	gorbital.Main(options()...)
}

// options are the app: what main.go runs and the tests build.
func options() []gorbital.Option {
	auth := authhttp.New() // sign-in: accounts, sessions, 2FA, passkeys, Google, Apple, GitHub, API keys
	return []gorbital.Option{
		gorbital.WithName("plateful"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(
			opshttp.Module(opshttp.MailProvider(mailProvider)), // /ops/: settings, flags, jobs, audit, email, observability
			flagshttp.Module(),  // GET /v1/flags: client feature flags
			mailevents.Module(), // POST /v1/webhooks/resend: bounces and complaints
		),
		gorbital.WithModules(orgshttp.Module(auth)), // organisations: members, roles, invitations, personal workspaces
		gorbital.WithModules(modules.All()...),      // internal/modules/modules.gen.go: ping, projects
		gorbital.WithMigrations(migrations.FS),      // db/migrations: the app's own tables
		gorbital.WithMailerFunc(mailer),             // mail.go: the email provider, set by orb add mail
		gorbital.WithStorageFunc(fileStorage),       // storage.go: S3-compatible file storage
	}
}
