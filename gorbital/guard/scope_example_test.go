package guard_test

import (
	"fmt"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
)

// guard.Scope protects a route with the app's own tenancy: the scope ID
// comes from the path parameter the app's gorbital.Scope declares, and the
// app's scope authorizer decides who may act in it.
func ExampleScope() {
	orders := gorbital.Module{
		Name: "orders",
		Permissions: []gorbital.Permission{
			{Name: "orders.order.read", Description: "See orders", ScopeRoles: []string{"owner", "manager"}},
			{Name: "orders.order.refund", Description: "Refund an order", ScopeRoles: []string{"owner"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// In an app built with gorbital.Scope{PathParam: "merchantId"}.
			g := r.Group("/v1/merchants/{merchantId}/orders", gorbital.Tags("Orders"))
			gorbital.Get(g, "/{id}", catalogBook, guard.Scope("orders.order.read"))
		},
	}
	for _, p := range orders.Permissions {
		fmt.Println(p.Name, p.ScopeRoles)
	}
	// Output:
	// orders.order.read [owner manager]
	// orders.order.refund [owner]
}
