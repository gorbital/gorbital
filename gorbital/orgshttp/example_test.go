package orgshttp_test

import (
	"embed"
	"fmt"
	"slices"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/gorbital/orgshttp"
	"gorbital.dev/mail"
)

// migrationFiles stands in for an app's db/migrations package.
var migrationFiles embed.FS

// modulesAll stands in for the generated internal/modules.All.
func modulesAll() []gorbital.Module { return nil }

func ExampleModule() {
	// cmd/api/main.go of a multi-tenant app: sign-in, /ops, organisations,
	// then the app's modules, whose routes use guard.Scope.
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
		orgRoles = append(orgRoles, p.ScopeRoles...)
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

func ExampleDefaultScope() {
	// The tenancy organisations give an app: guard.Scope refuses a
	// request for an organisation the caller isn't a member of with this
	// code, and reads the ID from this path parameter.
	s := orgshttp.DefaultScope()
	var roles []string
	for _, r := range s.Roles {
		roles = append(roles, r.Name)
	}
	fmt.Println(s.Name, s.PathParam, s.NotFoundCode, roles, s.ValidID("org_mfrggzdfmztwq2lkmfrggzdfmy"), s.ValidID("merchant_1234"))
	// Output: organisation orgId org_not_found [owner admin member] true false
}

// ownIdentity is an app's own sign-in: organisations need one method of
// it, so an app that doesn't use gorbital's can still mount them.
type ownIdentity struct{ hooks authhttp.Organisations }

func (o *ownIdentity) UseOrganisations(orgs authhttp.Organisations) error {
	o.hooks = orgs // a new account gets a personal workspace, and so on
	return nil
}

func ExampleIdentity() {
	// *authhttp.Authenticator is one, and so is an app's own sign-in.
	var _ orgshttp.Identity = authhttp.New()
	var id orgshttp.Identity = &ownIdentity{}
	// An identity that can't issue API keys leaves out the operations on
	// organisations' service accounts.
	fmt.Println(orgshttp.Module(id, orgshttp.WithoutServiceAccounts()).Name)
	// Output: orgs
}

func ExampleWithoutServiceAccounts() {
	// /v1/orgs/{orgId}/service-accounts and its API keys are sign-in's
	// operations; an app whose identity doesn't issue keys leaves them
	// out, with the orgs.service_accounts.manage permission.
	m := orgshttp.Module(authhttp.New(), orgshttp.WithoutServiceAccounts())
	var names []string
	for _, p := range m.Permissions {
		names = append(names, p.Name)
	}
	fmt.Println(slices.Contains(names, "orgs.service_accounts.manage"))
	// Output: false
}

func ExampleScopeName() {
	// A shop API mounts the same organisations as merchants: the routes
	// are /v1/merchants/{merchantId}/…, a request from someone who isn't
	// a member is refused with merchant_not_found, and the OpenAPI tag is
	// Merchants. The tables, migrations, permission names and role names
	// are the organisations module's, unchanged.
	auth := authhttp.New()
	m := orgshttp.Module(auth,
		orgshttp.ScopeName("merchant", "merchants", "merchantId"),
		orgshttp.WithoutServiceAccounts(), // sign-in registers those under /v1/orgs
	)
	var roles []string
	for _, p := range m.Permissions {
		roles = append(roles, p.ScopeRoles...)
	}
	slices.Sort(roles)
	fmt.Println(m.Name, slices.Compact(roles))
	// Output: orgs [admin member owner]
}
