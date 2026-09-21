package orgshttp_test

import (
	"embed"
	"fmt"
	"slices"

	authhttp "example.com/shelfie/internal/modules/auth"
	orgshttp "example.com/shelfie/internal/modules/orgs"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/mail"
)

// migrationFiles stands in for an app's db/migrations package.
var migrationFiles embed.FS

// modulesAll stands in for the generated internal/modules.All.
func modulesAll() []gorbital.Module { return nil }

func ExampleModule() {
	// cmd/api/main.go of a multi-tenant app: sign-in, /ops, organisations,
	// then the app's modules, whose routes use guard.OrgMember.
	main := func() {
		auth := authhttp.New()
		gorbital.Main(
			gorbital.WithAuth(auth),
			gorbital.WithModules(opshttp.Module(), orgshttp.Module(auth)),
			gorbital.WithModules(modulesAll()...),
			gorbital.WithMigrations(migrationFiles),
		)
	}
	_ = main

	m := orgshttp.Module(authhttp.New())
	var orgRoles []string
	for _, p := range m.Permissions {
		orgRoles = append(orgRoles, p.OrgRoles...)
	}
	slices.Sort(orgRoles)
	fmt.Println(m.Name, slices.Compact(orgRoles))
	for _, migration := range m.Migrations {
		fmt.Println(migration.Version, migration.Name)
	}
	// Output:
	// orgs [admin member owner]
	// 20260916000001 orgs
	// 20260918000002 settings_org_purge
}

func ExampleBrand() {
	// Invitations look like sign-in's emails when both get the same brand.
	brand := mail.Brand{Name: "Acme", URL: "https://acme.example.com", SupportEmail: "help@acme.example.com"}
	auth := authhttp.New(authhttp.Brand(brand))
	_ = gorbital.WithModules(orgshttp.Module(auth, orgshttp.Brand(brand)))
}

func ExampleOption() {
	var opts []orgshttp.Option
	opts = append(opts, orgshttp.Brand(mail.Brand{Name: "Acme"}))
	fmt.Println(orgshttp.Module(authhttp.New(), opts...).Name)
	// Output: orgs
}
