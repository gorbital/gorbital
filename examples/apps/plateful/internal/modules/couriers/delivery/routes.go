// Package delivery is the couriers module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/couriers/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start courier-routes

// Register adds the couriers routes to r. There are two groups, and the
// guards are the lesson of this module.
//
//   - /v1/couriers is a courier's own: register, read and change your
//     profile. guard.OrgMember can never be the right guard here, and not
//     because it would be inconvenient: a courier belongs to no organisation
//     at all, so the question it asks — "is the caller a member of the
//     organisation in the path?" — has no true answer for any courier and
//     any organisation, and there is no {orgId} in the path for it to read.
//     What guards these routes is guard.Permission(PermManage), a platform
//     permission the "user" role holds, which proves only that somebody is
//     signed in. The rest — that this profile is that somebody's — is the
//     use case's job, done by comparing the actor with the row's user_id.
//     Not every table has an org_id, and this is what the routes over one
//     look like.
//
//   - /v1/orgs/{orgId}/couriers is a restaurant dispatching an order.
//     guard.OrgMember(PermDispatch) fits this one, but it is worth being
//     precise about what it proves. The couriers it lists belong to no
//     organisation, so this is a cross-tenant read: the guard does not scope
//     the result to the caller's restaurant, because there is nothing on a
//     courier to scope it by. It proves that the caller is staff of a real
//     restaurant, and that is all — which is why the response carries the
//     courier's ID, name and vehicle and not their sign-in account, that
//     being nobody's business but the courier's own.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}

	own := r.Group("/v1/couriers", gorbital.Tags("Couriers"))
	gorbital.Post(own, "", h.registerCourier, gorbital.OperationID("couriers-register"),
		gorbital.Summary("Register as a courier"),
		gorbital.Description("One profile per account: registering twice fails with `courier_already_registered`. A new courier starts off duty."),
		gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermManage))
	gorbital.Get(own, "/me", h.getCourier, gorbital.OperationID("couriers-get-me"),
		gorbital.Summary("Get your courier profile"),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermManage))
	gorbital.Patch(own, "/me", h.updateCourier, gorbital.OperationID("couriers-update-me"),
		gorbital.Summary("Change your courier profile"),
		gorbital.Description("Send the fields to change and the `version` you read. You can't go off duty while you are carrying an order: that is `courier_on_delivery`."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermManage))

	dispatch := r.Group("/v1/orgs/{orgId}/couriers", gorbital.Tags("Dispatch"))
	gorbital.Get(dispatch, "", h.listAvailableCouriers, gorbital.OperationID("couriers-available"),
		gorbital.Summary("List the couriers available right now"),
		gorbital.Description("The couriers on duty and carrying nothing, for a restaurant choosing who to send an order out with. They are the platform's couriers, not the organisation's: a courier delivers for many restaurants."),
		guard.OrgMember(usecase.PermDispatch))
}

// docs:end courier-routes
