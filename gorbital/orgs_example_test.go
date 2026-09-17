package gorbital_test

import (
	"context"
	"fmt"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/orgs"
)

func ExampleOrgGrants() {
	invoices := gorbital.Module{
		Name: "invoices",
		Permissions: []gorbital.Permission{
			{Name: "invoices.invoice.read", Description: "See invoices", OrgRoles: []string{orgs.RoleOwner, orgs.RoleAdmin, orgs.RoleMember}},
			{Name: "invoices.invoice.void", Description: "Void invoices", OrgRoles: []string{orgs.RoleOwner}},
		},
	}
	fmt.Println(gorbital.OrgGrants(orgs.RoleOwner, invoices))
	fmt.Println(gorbital.OrgGrants(orgs.RoleMember, invoices))
	fmt.Println(gorbital.Grants(orgs.RoleOwner, invoices)) // platform roles hold none of them
	// Output:
	// [invoices.invoice.read invoices.invoice.void]
	// [invoices.invoice.read]
	// []
}

// memberships is an organisation authorizer over a fixed list of members,
// holding every permission they ask for.
type memberships map[string]string // "org/user" → role

func (m memberships) AuthorizeOrg(ctx context.Context, orgID, permission string) (context.Context, error) {
	a, ok := actor.From(ctx)
	if !ok {
		return ctx, actor.ErrUnauthenticated
	}
	if _, member := m[orgID+"/"+a.ID]; !member {
		return ctx, orgs.ErrOrgNotFound
	}
	a.OrgID, a.Permissions = orgID, []string{permission}
	return actor.With(ctx, a), nil
}

func ExampleOrgAuthorizer() {
	var authorizer gorbital.OrgAuthorizer = memberships{"org_1/usr_ada": orgs.RoleOwner}
	ada := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_ada"})
	ctx, err := authorizer.AuthorizeOrg(ada, "org_1", "invoices.invoice.read")
	a, _ := actor.From(ctx)
	fmt.Println(a.OrgID, err)
	_, err = authorizer.AuthorizeOrg(ada, "org_2", "invoices.invoice.read")
	fmt.Println(err)
	// Output:
	// org_1 <nil>
	// orgs: organisation not found
}

func ExamplePlatform_SetOrgAuthorizer() {
	// An organisations module makes itself the authorizer guard.OrgMember
	// asks, once the app is built. gorbital.dev/gorbital/orgshttp does.
	orgsModule := gorbital.Module{
		Name: "orgs",
		Platform: func(p *gorbital.Platform) error {
			return p.SetOrgAuthorizer(memberships{})
		},
	}
	fmt.Println(orgsModule.Name)
	// Output: orgs
}
