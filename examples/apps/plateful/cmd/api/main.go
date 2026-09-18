// Command api runs Plateful, a restaurant delivery platform where every
// restaurant is an organisation and customers and couriers belong to none.
// Built on gorbital.Main.
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
	"example.com/plateful/internal/modules/notifications"
)

func main() {
	gorbital.Main(options()...)
}

// docs:start main

// options are the app: what main.go runs and the tests build.
func options() []gorbital.Option {
	// Sign-in, with Plateful's own registration form: signin.go.
	auth := authhttp.New(signInOptions()...)
	return []gorbital.Option{
		gorbital.WithName("plateful"),
		gorbital.WithAuth(auth),
		gorbital.WithModules(
			opshttp.Module(opshttp.MailProvider(mailProvider)), // /ops/: settings, flags, jobs, audit, email, observability
			flagshttp.Module(),  // GET /v1/flags: the client feature flags the customer app reads
			mailevents.Module(), // POST /v1/webhooks/resend: bounces and complaints
		),
		// Restaurants are organisations: members, roles and invitations are
		// how a restaurateur adds their manager and their kitchen staff.
		gorbital.WithModules(orgshttp.Module(auth)),
		// internal/modules/modules.gen.go: couriers, images, menus, orders,
		// payments, restaurants, reviews.
		gorbital.WithModules(modules.All()...),
		// Notifications takes an option, so orb gen modules leaves it out of
		// modules.All and main.go adds it here. What it configures is the
		// one thing that must not be operator-editable: whether the outbound
		// sender may reach an address on this machine (notifications.go).
		gorbital.WithModules(notifications.Module(webhookTargets())),
		gorbital.WithMigrations(migrations.FS), // db/migrations: the app's own tables
		gorbital.WithMailerFunc(mailer),        // mail.go: the email provider, set by orb add mail
		gorbital.WithStorageFunc(fileStorage),  // storage.go: S3-compatible storage for menu photos
	}
}

// docs:end main
