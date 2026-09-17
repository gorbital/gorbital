// Package delivery is the invoices module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/invoicing/internal/modules/invoices/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// Register adds the invoices routes to r, under an organisation. Every
// route requires a signed-in member of the organisation in the path whose
// role grants the permission its guard names (guard.OrgMember): anyone else
// gets 404 org_not_found, as if the organisation didn't exist. The use cases
// reach only that organisation's invoices.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	invoices := r.Group("/v1/orgs/{orgId}/invoices", gorbital.Tags("Invoices"))

	gorbital.Post(invoices, "", h.createInvoice, gorbital.OperationID("invoices-create"),
		gorbital.Summary("Create an invoice"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Get(invoices, "", h.listInvoices, gorbital.OperationID("invoices-list"),
		gorbital.Summary("List the organisation's invoices"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at`, `number` or `customer`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermRead))
	gorbital.Get(invoices, "/{id}", h.getInvoice, gorbital.OperationID("invoices-get"),
		gorbital.Summary("Get an invoice"),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermRead))
	gorbital.Patch(invoices, "/{id}", h.updateInvoice, gorbital.OperationID("invoices-update"),
		gorbital.Summary("Update an invoice"),
		gorbital.Description("Send the fields to change and the `version` you read."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Delete(invoices, "/{id}", h.deleteInvoice, gorbital.OperationID("invoices-delete"),
		gorbital.Summary("Delete an invoice"), gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermWrite))
}
