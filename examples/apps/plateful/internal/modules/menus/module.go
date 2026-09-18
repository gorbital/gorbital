// Package menus is the menus module: the dishes a restaurant sells, in four
// layers (domain, usecase, repository, delivery) with one file per operation
// in each.
//
// A menu reads as sections of dishes, and it is stored as one table of
// dishes that each carry their section's name and their place in it. One
// table is enough because a section is not a thing a restaurant owns: it has
// no settings, no lifecycle, no ID a customer or another module ever quotes,
// and nothing points at it. A sections table would buy an empty "Desserts"
// that outlives its last pudding, a join on the busiest read in the app, and
// a second write whenever a dish moves between headings. Carrying the name
// on the row makes a rename, a reorder and a move a single UPDATE, and the
// menu read one index scan that the use case groups in a single pass
// (domain.GroupSections). The day a section grows a photo, a schedule or a
// description of its own is the day it has earned a table; it has not.
package menus

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/menus/delivery"
	"example.com/plateful/internal/modules/menus/domain"
	"example.com/plateful/internal/modules/menus/repository"
	"example.com/plateful/internal/modules/menus/usecase"
)

// Module returns the menus module. main.go adds it with every other module
// through modules.All; its staff routes need the organisations module
// (orgshttp.Module), which answers guard.OrgMember, and its customer route
// needs a restaurant to have been published by the restaurants module. Error
// codes and permission names are public API: add new ones, never change
// existing ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "menus",
		// docs:start menu-errors
		// guard.OrgMember answers org_not_found for an organisation the
		// caller isn't a member of, and forbidden for a role without the
		// permission, so neither is listed here.
		//
		// restaurant_not_found is the customer's answer for a restaurant
		// that isn't open, whatever the reason: it doesn't exist, it never
		// published, it is paused for the night, or platform staff suspended
		// it. One answer for all four is the point — a suspension is
		// nobody's business but the platform's and the restaurant's, and
		// telling the cases apart would let anyone find out which
		// restaurants are in trouble by asking for their menus.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidItem, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the menu item is not valid"},
			{Err: domain.ErrItemNotFound, Status: http.StatusNotFound, Code: "menu_item_not_found", Detail: "the organisation has no menu item with this ID"},
			{Err: domain.ErrItemNameTaken, Status: http.StatusConflict, Code: "menu_item_name_taken", Detail: "the menu already has a dish with this name"},
			{Err: domain.ErrItemVersionConflict, Status: http.StatusConflict, Code: "menu_item_version_conflict", Detail: "the menu item changed since you read it; get it again and retry"},
			{Err: domain.ErrRestaurantNotFound, Status: http.StatusNotFound, Code: "restaurant_not_found", Detail: "no open restaurant has this ID"},
		},
		// docs:end menu-errors
		// docs:start menu-permissions
		// The first two are organisation permissions: a member holds them
		// through their role in the restaurant's organisation, an API key
		// only when its scopes include them. Platform roles grant nothing in
		// an organisation.
		//
		// The third is a platform permission, and it has to be: the customer
		// who reads a menu belongs to no organisation, so there is no
		// organisation role to give them. Every signed-in account holds
		// "user", which is what makes the published menu readable by
		// customers and by nobody who isn't signed in.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the organisation's menu", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermWrite, Description: "Add, change and delete the organisation's dishes", OrgRoles: []string{"owner", "admin"}},
			{Name: usecase.PermBrowse, Description: "Read an open restaurant's published menu", Roles: []string{"user"}},
		},
		// docs:end menu-permissions
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
