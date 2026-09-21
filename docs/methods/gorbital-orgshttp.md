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

Organisations are one tenancy an app can have (ADR-0088): the module makes [DefaultScope](#DefaultScope) the app's scope, so app modules scope their routes to an organisation with guard.Scope, which asks this module whether the caller is a member whose role grants a permission, and declare their permissions with gorbital.Permission's ScopeRoles:

```go
invoices := r.Group("/v1/orgs/{orgId}/invoices")
gorbital.Get(invoices, "", h.list, guard.Scope("invoices.invoice.read"))
```

[Module](#Module) takes an [Identity](#Identity) rather than a particular sign-in.

Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).

## Contents

- Functions: [`DefaultScope`](#DefaultScope), [`Module`](#Module)
- Types:
  - [`Identity`](#Identity)
  - [`Option`](#Option): [`Brand`](#Brand), [`WithoutServiceAccounts`](#WithoutServiceAccounts)

## Functions

<a id="DefaultScope"></a>

### func DefaultScope

```go
func DefaultScope() gorbital.Scope
```

DefaultScope is the tenancy organisations give an app (ADR-0088): the concept named "organisation", its ID in {orgId} and shaped like orgs.ParseID accepts, refused with 404 org\_not\_found, the roles owner, admin and member, and the organisation carried into the request's connections for row-level security. It is the vocabulary of v0.1 and v0.2 multi-tenant apps, so their HTTP contract doesn't change.

[Module](#Module) sets it with gorbital.Platform.SetScope, so an app that mounts organisations needs nothing else for guard.Scope. An app that writes its own membership rules starts from its own gorbital.Scope instead.

Unlike gorbital.DefaultOrgScope it has a ValidID: only this package may import gorbital.dev/modules/orgs, so only this package knows how an organisation ID is shaped.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// The tenancy organisations give an app: guard.Scope refuses a
// request for an organisation the caller isn't a member of with this
// code, and reads the ID from this path parameter.
s := orgshttp.DefaultScope()
var roles []string
for _, r := range s.Roles {
	roles = append(roles, r.Name)
}
fmt.Println(s.Name, s.PathParam, s.NotFoundCode, roles, s.ValidID("org_mfrggzdfmztwq2lkmfrggzdfmy"), s.ValidID("merchant_1234"))
```

Output:

```text
organisation orgId org_not_found [owner admin member] true false
```

<a id="Module"></a>

### func Module

```go
func Module(id Identity, opts ...Option) gorbital.Module
```

Module returns organisations as a module named "orgs". id is the app's sign-in, the value passed to gorbital.WithAuth when that is gorbital.dev/gorbital/authhttp: organisation members are its accounts (org\_members references auth\_users), a new account gets a personal workspace, deleting an account leaves or deletes its organisations, and organisations' service accounts are sign-in's.

gorbital.New fails with a configuration error when id is nil, or is the app's authenticator's type and not the app's authenticator. Mounted by hand with gorbital.Mount, the module registers its routes for the OpenAPI document only.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
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
```

Output:

```text
orgs [admin member owner]
20260916000001 orgs
20260918000002 settings_org_purge
```

## Types

<a id="Identity"></a>
<a id="Identity.UseOrganisations"></a>

### type Identity

```go
type Identity interface {
	// UseOrganisations connects the identity to o: a new account gets a
	// personal workspace, deleting an account is refused while it is an
	// organisation's only owner and otherwise leaves its organisations,
	// and o authorizes the organisations whose service accounts the
	// identity manages. It is called once, while the app is built.
	UseOrganisations(o authhttp.Organisations) error
}
```

An Identity is what organisations need from the app's sign-in: organisation members are its accounts (org\_members references auth\_users), so it tells organisations about accounts as they are created and deleted, and asks them before deleting one. \*authhttp.Authenticator is one, and nothing in the library is any other: the interface is the seam that keeps organisations from requiring gorbital's own sign-in (ADR-0088, ADR-0092).

An identity that issues API keys also has

```go
OrgServiceAccountRoutes(r *gorbital.Router)
```

which registers the operations on organisations' service accounts; [WithoutServiceAccounts](#WithoutServiceAccounts) leaves them out for an identity that doesn't.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// *authhttp.Authenticator is one, and so is an app's own sign-in.
var _ orgshttp.Identity = authhttp.New()
var id orgshttp.Identity = &ownIdentity{}
// An identity that can't issue API keys leaves out the operations on
// organisations' service accounts.
fmt.Println(orgshttp.Module(id, orgshttp.WithoutServiceAccounts()).Name)
```

Output:

```text
orgs
```

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

<a id="WithoutServiceAccounts"></a>

#### func WithoutServiceAccounts

```go
func WithoutServiceAccounts() Option
```

WithoutServiceAccounts leaves out the operations on organisations' service accounts and their API keys (/v1/orgs/{orgId}/service-accounts, ADR-0058) and the orgs.service\_accounts.manage permission that guards them.

They are sign-in's operations, registered through the [Identity](#Identity)'s OrgServiceAccountRoutes: an identity that can't issue API keys doesn't have that method. Without this option gorbital.New fails, naming it.

*Since `v0.2.0 (unreleased)`*

**Example**

```go
// /v1/orgs/{orgId}/service-accounts and its API keys are sign-in's
// operations; an app whose identity doesn't issue keys leaves them
// out, with the orgs.service_accounts.manage permission.
m := orgshttp.Module(authhttp.New(), orgshttp.WithoutServiceAccounts())
var names []string
for _, p := range m.Permissions {
	names = append(names, p.Name)
}
fmt.Println(slices.Contains(names, "orgs.service_accounts.manage"))
```

Output:

```text
false
```
