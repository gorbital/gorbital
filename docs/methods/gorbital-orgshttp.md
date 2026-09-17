# gorbital/orgshttp

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

```go
import "gorbital.dev/gorbital/orgshttp"
```

Package orgshttp is organisations as a gorbital module, for multi-tenant apps (ADR-0023, ADR-0048): organisations with members holding one role each (owner, admin, member, and the roles the app's modules grant), invitations by email, a personal workspace per account, deletion with a restore period and the orgs\_purge job, each organisation's own runtime settings (ADR-0056) and client feature flags (ADR-0057), and organisations' service accounts with their API keys (ADR-0058).

It is the orgs module v0.1 multi-tenant apps generate into internal/modules/orgs, moved into the library: the same paths under /v1/orgs and /v1/invitations, operation IDs, schemas, error codes, audit actions, permissions and roles, runtime settings (orgs.\*), job and migrations. Add it in main.go with the app's sign-in, which it needs for accounts, personal workspaces and service accounts:

```go
auth := authhttp.New()
gorbital.Main(
	gorbital.WithAuth(auth),
	gorbital.WithModules(opshttp.Module(), orgshttp.Module(auth)),
	gorbital.WithModules(modules.All()...),
)
```

App modules scope their routes to an organisation with guard.OrgMember, which asks this module whether the caller is a member whose role grants a permission, and declare their permissions with gorbital.Permission's OrgRoles:

```go
invoices := r.Group("/v1/orgs/{orgId}/invoices")
gorbital.Get(invoices, "", h.list, guard.OrgMember("invoices.invoice.read"))
```

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Functions: [`Module`](#Module)
- Types:
  - [`Option`](#Option): [`Brand`](#Brand)

## Functions

<a id="Module"></a>

### func Module

```go
func Module(auth *authhttp.Authenticator, opts ...Option) gorbital.Module
```

Module returns organisations as a module named "orgs". auth is the app's sign-in, the value passed to gorbital.WithAuth: organisation members are its accounts (org\_members references auth\_users), a new account gets a personal workspace, deleting an account leaves or deletes its organisations, and organisations' service accounts are sign-in's.

gorbital.New fails with a configuration error when auth is nil or isn't the app's authenticator. Mounted by hand with gorbital.Mount, the module registers its routes for the OpenAPI document only.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

Output:

```text
orgs [admin member owner]
20260916000001 orgs
20260918000002 settings_org_purge
```

## Types

<a id="Option"></a>

### type Option

```go
type Option func(*options)
```

An Option configures [Module](#Module).

*Since `v0.2.0 (unreleased)`*

**Example**

```go
var opts []orgshttp.Option
opts = append(opts, orgshttp.Brand(mail.Brand{Name: "Acme"}))
fmt.Println(orgshttp.Module(authhttp.New(), opts...).Name)
```

Output:

```text
orgs
```

<a id="Brand"></a>

#### func Brand

```go
func Brand(b mail.Brand) Option
```

Brand sets what invitation emails have in common with the app's other emails: its name, link, logo, support address and footer (mail.Brand). Without it, invitations carry the app's name (gorbital.WithName) linking to APP\_PUBLIC\_URL, as sign-in's emails do without authhttp.Brand; pass the same value to both.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// Invitations look like sign-in's emails when both get the same brand.
brand := mail.Brand{Name: "Acme", URL: "https://acme.example.com", SupportEmail: "help@acme.example.com"}
auth := authhttp.New(authhttp.Brand(brand))
_ = gorbital.WithModules(orgshttp.Module(auth, orgshttp.Brand(brand)))
```
