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

// Register adds the records routes to r.
//
// Every record is world-readable: these routes serve them to callers who
// are not signed in. Do not add a field here that one record's owner would
// not publish.
//
// Writing is not public: create, update and delete require a signed-in
// caller with records.record.write, as they do in every other module.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	records := r.Group("/v1/records", gorbital.Tags("Records"))

	gorbital.Post(records, "", h.createRecord, gorbital.OperationID("records-create"),
		gorbital.Summary("Create a record"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))
	gorbital.Get(records, "", h.listRecords, gorbital.OperationID("records-list"),
		gorbital.Summary("List all records"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at` or `title`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.Public())
	gorbital.Get(records, "/{id}", h.getRecord, gorbital.OperationID("records-get"),
		gorbital.Summary("Get a record"),
		gorbital.Errors(http.StatusNotFound),
		guard.Public())
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
