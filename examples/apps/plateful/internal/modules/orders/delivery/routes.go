// Package delivery is the orders module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/orders/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// Register adds the orders routes to r, under an organisation. Every
// route requires a signed-in member of the organisation in the path whose
// role grants the permission its guard names (guard.OrgMember): anyone else
// gets 404 org_not_found, as if the organisation didn't exist. The use cases
// reach only that organisation's orders.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	orders := r.Group("/v1/orgs/{orgId}/orders", gorbital.Tags("Orders"))

	gorbital.Post(orders, "", h.createOrder, gorbital.OperationID("orders-create"),
		gorbital.Summary("Create an order"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Get(orders, "", h.listOrders, gorbital.OperationID("orders-list"),
		gorbital.Summary("List the organisation's orders"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at` or `address`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermRead))
	gorbital.Get(orders, "/{id}", h.getOrder, gorbital.OperationID("orders-get"),
		gorbital.Summary("Get an order"),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermRead))
	gorbital.Patch(orders, "/{id}", h.updateOrder, gorbital.OperationID("orders-update"),
		gorbital.Summary("Update an order"),
		gorbital.Description("Send the fields to change and the `version` you read."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Delete(orders, "/{id}", h.deleteOrder, gorbital.OperationID("orders-delete"),
		gorbital.Summary("Delete an order"), gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermWrite))
}
