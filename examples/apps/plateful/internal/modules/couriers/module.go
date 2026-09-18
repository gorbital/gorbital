// Package couriers is the couriers module: the people who carry the orders,
// in four layers (domain, usecase, repository, delivery) with one file per
// operation in each.
//
// It is the one resource on this platform that is not scoped to a tenant,
// and it is here to show what that costs. Every other table of Plateful has
// an org_id, because every other thing belongs to a restaurant; a courier is
// a person who delivers for many restaurants and is employed by none of
// them, so the couriers table has no org_id and there is no organisation on
// a courier to scope anything by.
//
// The consequence runs through the whole module. guard.OrgMember can never
// be the right guard for a courier's own routes: it answers "is the caller a
// member of the organisation in the path", and for a courier there is no
// organisation and no path to put one in, so the question is not merely
// inconvenient, it is meaningless. Those routes are guarded with
// guard.Permission(usecase.PermManage) instead — a platform permission the
// "user" role holds, which proves only that somebody is signed in — and the
// ownership check is done in the use case, by comparing the signed-in
// account with the courier row's user_id. The one route that does use
// guard.OrgMember, the restaurant's dispatch list, is a cross-tenant read:
// see delivery/routes.go.
//
// Nothing is exported from this package for other modules. The orders module
// reads and writes the couriers table with its own SQL, inside its own
// transaction, because that is the only way it can lock the courier row and
// the order row together; a Go helper here could not be part of that
// transaction without handing out this module's internals, and the table's
// column names (id, user_id, available, active_order_id) are the contract
// instead.
package couriers

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/couriers/delivery"
	"example.com/plateful/internal/modules/couriers/domain"
	"example.com/plateful/internal/modules/couriers/repository"
	"example.com/plateful/internal/modules/couriers/usecase"
)

// Module returns the couriers module. main.go adds it with every other
// module through modules.All; the dispatch route needs the organisations
// module (orgshttp.Module), which answers guard.OrgMember. Error codes and
// permission names are public API: add new ones, never change existing ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "couriers",
		// docs:start courier-errors
		//
		// A courier's own routes can't answer org_not_found, the refusal
		// every org-scoped route in this app leans on, because they have no
		// organisation to not find. A profile that isn't the caller's is
		// courier_not_found instead, so that one account can't discover
		// another by asking; the dispatch route, which does sit under an
		// organisation, still gets org_not_found from guard.OrgMember.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidCourier, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the courier is not valid"},
			{Err: domain.ErrCourierNotFound, Status: http.StatusNotFound, Code: "courier_not_found", Detail: "you are not registered as a courier"},
			{Err: domain.ErrCourierAlreadyRegistered, Status: http.StatusConflict, Code: "courier_already_registered", Detail: "this account is already registered as a courier"},
			{Err: domain.ErrCourierOnDelivery, Status: http.StatusConflict, Code: "courier_on_delivery", Detail: "you are carrying an order: finish the delivery before going off duty"},
			{Err: domain.ErrCourierVersionConflict, Status: http.StatusConflict, Code: "courier_version_conflict", Detail: "your profile changed since you read it; get it again and retry"},
		},
		// docs:end courier-errors
		//
		// docs:start courier-permission-catalog
		//
		// The two permissions of this module are declared in two different
		// catalogs, and the declaration is what decides how each is held.
		// Roles puts a permission in the platform catalog, where "user" is
		// every signed-in account: that is the only kind of permission a
		// courier can hold, because holding an organisation permission means
		// acting in an organisation and a courier never does. OrgRoles puts
		// one in the organisation catalog, held by a restaurant's own staff
		// through their role in it. A permission has one or the other, never
		// both.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermManage, Description: "Register as a courier and manage your own courier profile", Roles: []string{"user"}},
			{Name: usecase.PermDispatch, Description: "See the couriers available to deliver the organisation's orders", OrgRoles: []string{"owner", "admin", "member"}},
		},
		// docs:end courier-permission-catalog
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
