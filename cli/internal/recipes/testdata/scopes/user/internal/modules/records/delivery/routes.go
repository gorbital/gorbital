// Package delivery is the records module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/app/internal/modules/records/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// Register adds the records routes to r. Every route requires a signed-in
// user (deny by default) and the permission its guard names; the use cases
// reach only that user's records.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	records := r.Group("/v1/records", gorbital.Tags("Records"))

	gorbital.Post(records, "", h.createRecord, gorbital.OperationID("records-create"),
		gorbital.Summary("Create a record"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))
	gorbital.Get(records, "", h.listRecords, gorbital.OperationID("records-list"),
		gorbital.Summary("List your records"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at` or `title`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermRead))
	gorbital.Get(records, "/{id}", h.getRecord, gorbital.OperationID("records-get"),
		gorbital.Summary("Get a record"),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermRead))
	gorbital.Patch(records, "/{id}", h.updateRecord, gorbital.OperationID("records-update"),
		gorbital.Summary("Update a record"),
		gorbital.Description("Send the fields to change and the `version` you read."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))
	gorbital.Delete(records, "/{id}", h.deleteRecord, gorbital.OperationID("records-delete"),
		gorbital.Summary("Delete a record"), gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermWrite))
}
