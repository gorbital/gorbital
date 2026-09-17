// Package delivery is the shelves module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/shelfie/internal/modules/shelves/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// Register adds the shelves routes to r. Every route requires a signed-in
// user (deny by default) and the permission its guard names; the use cases
// reach only that user's shelves.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	shelves := r.Group("/v1/shelves", gorbital.Tags("Shelves"))

	gorbital.Post(shelves, "", h.createShelf, gorbital.OperationID("shelves-create"),
		gorbital.Summary("Create a shelf"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))
	gorbital.Get(shelves, "", h.listShelves, gorbital.OperationID("shelves-list"),
		gorbital.Summary("List your shelves"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at` or `name`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermRead))
	gorbital.Get(shelves, "/{id}", h.getShelf, gorbital.OperationID("shelves-get"),
		gorbital.Summary("Get a shelf"),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermRead))
	gorbital.Patch(shelves, "/{id}", h.updateShelf, gorbital.OperationID("shelves-update"),
		gorbital.Summary("Update a shelf"),
		gorbital.Description("Send the fields to change and the `version` you read."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))
	gorbital.Delete(shelves, "/{id}", h.deleteShelf, gorbital.OperationID("shelves-delete"),
		gorbital.Summary("Delete a shelf"), gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermWrite))
}
