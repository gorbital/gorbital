// Package delivery is the menus module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/menus/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start menu-routes

// Register adds the menus routes to r. There are two groups, and the guard
// is what tells them apart.
//
//   - /v1/orgs/{orgId}/menu/items is the restaurant's own staff.
//     guard.OrgMember checks that the caller is a member of that
//     organisation and that their role holds the permission; anyone else
//     gets 404 org_not_found, as if the organisation didn't exist. This is
//     the whole menu, the dishes the kitchen has switched off included.
//   - /v1/restaurants/{restaurantId}/menu is the customers'. A customer is
//     in no organisation, so guard.OrgMember could never let them in;
//     guard.Permission checks a platform permission the "user" role holds,
//     and the use case decides which menu they may read and which of its
//     dishes they are shown.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}

	staff := r.Group("/v1/orgs/{orgId}/menu/items", gorbital.Tags("Menu"))
	gorbital.Post(staff, "", h.createItem, gorbital.OperationID("menus-create-item"),
		gorbital.Summary("Add a dish to your menu"),
		gorbital.Description("`price_minor` is the price in minor units: 950 is £9.50. Leave `stock` out for a dish you never run out of."),
		gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Get(staff, "", h.listItems, gorbital.OperationID("menus-list-items"),
		gorbital.Summary("List your whole menu"),
		gorbital.Description("Every dish, available or not. By name unless `sort` says otherwise; sort by `name`, `price_minor` or `created_at`. Filter with `section` and `available`, paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermRead))
	gorbital.Get(staff, "/{id}", h.getItem, gorbital.OperationID("menus-get-item"),
		gorbital.Summary("Get one dish"),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermRead))
	gorbital.Patch(staff, "/{id}", h.updateItem, gorbital.OperationID("menus-update-item"),
		gorbital.Summary("Change a dish"),
		gorbital.Description("Send the fields to change and the `version` you read. Set `available` to false when the kitchen runs out; the dish stays on your menu and leaves the customers' one."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Delete(staff, "/{id}", h.deleteItem, gorbital.OperationID("menus-delete-item"),
		gorbital.Summary("Delete a dish"),
		gorbital.Description("For a dish that should never have existed. To stop selling one tonight, set `available` to false instead."),
		gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermWrite))

	public := r.Group("/v1/restaurants/{restaurantId}", gorbital.Tags("Restaurants"))
	gorbital.Get(public, "/menu", h.viewMenu, gorbital.OperationID("menus-view-menu"),
		gorbital.Summary("Read an open restaurant's menu"),
		gorbital.Description("The published menu: sections in order, each with the dishes available right now. A restaurant that isn't open answers 404."),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermBrowse))
}

// docs:end menu-routes
