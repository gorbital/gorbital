// Package delivery is the restaurants module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
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

// Register adds the restaurants routes to r, under an organisation. Every
// route requires a signed-in member of the organisation in the path whose
// role grants the permission its guard names (guard.OrgMember): anyone else
// gets 404 org_not_found, as if the organisation didn't exist. The use cases
// reach only that organisation's restaurants.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	restaurants := r.Group("/v1/orgs/{orgId}/restaurants", gorbital.Tags("Restaurants"))

	gorbital.Post(restaurants, "", h.createRestaurant, gorbital.OperationID("restaurants-create"),
		gorbital.Summary("Create a restaurant"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Get(restaurants, "", h.listRestaurants, gorbital.OperationID("restaurants-list"),
		gorbital.Summary("List the organisation's restaurants"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at`, `name`, `address` or `cuisine`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermRead))
	gorbital.Get(restaurants, "/{id}", h.getRestaurant, gorbital.OperationID("restaurants-get"),
		gorbital.Summary("Get a restaurant"),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermRead))
	gorbital.Patch(restaurants, "/{id}", h.updateRestaurant, gorbital.OperationID("restaurants-update"),
		gorbital.Summary("Update a restaurant"),
		gorbital.Description("Send the fields to change and the `version` you read."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Delete(restaurants, "/{id}", h.deleteRestaurant, gorbital.OperationID("restaurants-delete"),
		gorbital.Summary("Delete a restaurant"), gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermWrite))
}
