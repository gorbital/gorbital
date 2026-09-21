package gorbital_test

import (
	"context"
	"fmt"
	"strings"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/postgres"
)

// merchantScope is the tenancy of an app whose customers are merchants:
// its own word, its own IDs, its own roles.
func merchantScope() gorbital.Scope {
	return gorbital.Scope{
		Name:         "merchant",
		PathParam:    "merchantId",
		NotFoundCode: "merchant_not_found",
		ValidID:      func(id string) bool { return strings.HasPrefix(id, "mch_") },
		Roles: []gorbital.ScopeRole{
			{Name: "owner", Description: "Runs the merchant"},
			{Name: "manager", Description: "Manages staff and orders"},
			{Name: "courier", Description: "Delivers orders"},
		},
		Session: postgres.WithScope,
	}
}

func ExampleScope() {
	s := merchantScope()
	fmt.Println(s.Name, s.PathParam, s.NotFoundCode)
	// Routes carry the ID in the scope's own parameter.
	fmt.Println("/v1/merchants/{" + s.PathParam + "}/orders")
	// Output:
	// merchant merchantId merchant_not_found
	// /v1/merchants/{merchantId}/orders
}

func ExampleScopeRole() {
	for _, r := range merchantScope().Roles {
		fmt.Printf("%s: %s\n", r.Name, r.Description)
	}
	// Output:
	// owner: Runs the merchant
	// manager: Manages staff and orders
	// courier: Delivers orders
}

// members is a scope authorizer over the app's own membership table.
type members map[string]string // "merchant/user" → role

func (m members) AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error) {
	a, ok := actor.From(ctx)
	if !ok {
		return ctx, actor.ErrUnauthenticated
	}
	role, member := m[scopeID+"/"+a.ID]
	if !member {
		return ctx, gorbital.ErrScopeNotFound
	}
	if role != "owner" && permission == "orders.order.refund" {
		return ctx, actor.ErrForbidden
	}
	a.OrgID, a.Permissions = scopeID, []string{permission}
	return actor.With(ctx, a), nil
}

func ExampleScopeAuthorizer() {
	var authorizer gorbital.ScopeAuthorizer = members{
		"mch_1/usr_ada": "owner",
		"mch_1/usr_bo":  "courier",
	}
	ada := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_ada"})
	bo := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_bo"})

	ctx, err := authorizer.AuthorizeScope(ada, "mch_1", "orders.order.refund")
	a, _ := actor.From(ctx)
	fmt.Println(a.OrgID, err)

	_, err = authorizer.AuthorizeScope(bo, "mch_1", "orders.order.refund")
	fmt.Println(err)

	_, err = authorizer.AuthorizeScope(ada, "mch_2", "orders.order.read")
	fmt.Println(err)
	// Output:
	// mch_1 <nil>
	// actor: permission denied
	// gorbital: scope not found
}

func ExampleWithScope() {
	// An app with its own membership tables gives gorbital the vocabulary
	// and the authorizer; guard.Scope asks it on every scope route.
	opts := []gorbital.Option{
		gorbital.WithName("shop-api"),
		gorbital.WithScope(merchantScope(), members{}),
	}
	fmt.Println(len(opts), "options")
	// Output: 2 options
}

func ExampleWithScopeWords() {
	// An app whose membership is a module's, mounted under its own words:
	// the module still sets the scope, and this says the words before it
	// runs, for guard.Scope's route checks and the exported OpenAPI
	// document.
	opts := []gorbital.Option{
		gorbital.WithName("shop-api"),
		gorbital.WithScopeWords(gorbital.Scope{
			Name:         "merchant",
			PathParam:    "merchantId",
			NotFoundCode: "merchant_not_found",
		}),
	}
	fmt.Println(len(opts), "options")
	// Output: 2 options
}

func ExamplePlatform_SetScope() {
	// A module that owns membership sets the scope once the app is built.
	// gorbital.dev/gorbital/orgshttp does this for organisations.
	merchants := gorbital.Module{
		Name: "merchants",
		Platform: func(p *gorbital.Platform) error {
			return p.SetScope(merchantScope(), members{})
		},
	}
	fmt.Println(merchants.Name)
	// Output: merchants
}

func ExampleDefaultOrgScope() {
	s := gorbital.DefaultOrgScope()
	fmt.Println(s.Name, s.PathParam, s.NotFoundCode)
	for _, r := range s.Roles {
		fmt.Print(r.Name, " ")
	}
	fmt.Println()
	// Output:
	// organisation orgId org_not_found
	// owner admin member
}

func ExampleScopeGrants() {
	orders := gorbital.Module{
		Name: "orders",
		Permissions: []gorbital.Permission{
			{Name: "orders.order.read", Description: "See orders", ScopeRoles: []string{"owner", "manager", "courier"}},
			{Name: "orders.order.refund", Description: "Refund an order", ScopeRoles: []string{"owner"}},
		},
	}
	fmt.Println(gorbital.ScopeGrants("owner", orders))
	fmt.Println(gorbital.ScopeGrants("courier", orders))
	fmt.Println(gorbital.Grants("owner", orders)) // platform roles hold none of them
	// Output:
	// [orders.order.read orders.order.refund]
	// [orders.order.read]
	// []
}
