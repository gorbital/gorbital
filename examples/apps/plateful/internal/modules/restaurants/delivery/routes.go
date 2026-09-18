// Package delivery is the restaurants module's HTTP adapter: the route
// table in this file, and one file per operation with its input, output and
// handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/restaurants/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start restaurant-routes

// Register adds the restaurants routes to r. They fall into three groups,
// and the guard is what tells them apart.
//
//   - /v1/orgs/{orgId}/restaurant is the restaurant's own staff: guard.OrgMember
//     checks that the caller is a member of that organisation and that their
//     role holds the permission. Anyone else gets 404 org_not_found, as if
//     the organisation didn't exist.
//   - /v1/restaurants is every signed-in account, customers included.
//     Customers belong to no organisation, so guard.OrgMember could never let
//     them in; guard.Permission checks a platform permission that the "user"
//     role holds, and the use case decides which restaurants they may see.
//   - /v1/platform/restaurants is the platform's own staff. The path carries
//     no organisation because the operation isn't a tenant's: it is the
//     platform acting on one.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}

	staff := r.Group("/v1/orgs/{orgId}/restaurant", gorbital.Tags("Restaurant"))
	gorbital.Get(staff, "", h.getRestaurant, gorbital.OperationID("restaurants-get"),
		gorbital.Summary("Get your restaurant's profile"),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermRead))
	gorbital.Put(staff, "", h.saveRestaurant, gorbital.OperationID("restaurants-save"),
		gorbital.Summary("Create or update your restaurant's profile"),
		gorbital.Description("Send `version: 0` to create the profile the organisation doesn't have yet, and the version you read to change it. `status` is one of `onboarding`, `open` or `paused`; only platform staff suspend a restaurant."),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))

	public := r.Group("/v1/restaurants", gorbital.Tags("Restaurants"))
	gorbital.Get(public, "", h.browseRestaurants, gorbital.OperationID("restaurants-browse"),
		gorbital.Summary("Browse the open restaurants"),
		gorbital.Description("By name unless `sort` says otherwise; sort by `name` or `created_at`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest),
		guard.Permission(usecase.PermBrowse))
	gorbital.Get(public, "/{id}", h.viewRestaurant, gorbital.OperationID("restaurants-view"),
		gorbital.Summary("Get an open restaurant"),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermBrowse))

	platform := r.Group("/v1/platform/restaurants", gorbital.Tags("Platform"))
	gorbital.Get(platform, "", h.overseeRestaurants, gorbital.OperationID("restaurants-oversee"),
		gorbital.Summary("List every restaurant on the platform"),
		gorbital.Errors(http.StatusBadRequest),
		guard.Permission(usecase.PermOversee))
	gorbital.Post(platform, "/{id}/suspend", h.suspendRestaurant, gorbital.OperationID("restaurants-suspend"),
		gorbital.Summary("Suspend a restaurant"),
		gorbital.Description("Stops the restaurant taking orders at once and records why. Platform staff only."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermSuspend), guard.RecentReauth())
	gorbital.Post(platform, "/{id}/unsuspend", h.liftSuspension, gorbital.OperationID("restaurants-unsuspend"),
		gorbital.Summary("Lift a restaurant's suspension"),
		gorbital.Description("The restaurant comes back paused; its own staff decide when it opens again."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict),
		guard.Permission(usecase.PermSuspend), guard.RecentReauth())
}

// docs:end restaurant-routes
