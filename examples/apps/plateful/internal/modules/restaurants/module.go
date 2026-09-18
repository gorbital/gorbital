// Package restaurants is the restaurants module: restaurants that belong to an
// organisation, whose members reach them through their role, in four layers
// (domain, usecase, repository, delivery) with one file per operation in
// each. orb gen module wrote it; the code is yours to change.
package restaurants

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/restaurants/delivery"
	"example.com/plateful/internal/modules/restaurants/domain"
	"example.com/plateful/internal/modules/restaurants/repository"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

// Module returns the restaurants module. main.go adds it with every other
// module through modules.All; its routes need the organisations module
// (orgshttp.Module), which answers guard.OrgMember. Error codes and
// permission names are public API: add new ones, never change existing
// ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "restaurants",
		// guard.OrgMember answers org_not_found for an organisation the caller
		// isn't a member of, and forbidden for a role without the permission.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidRestaurant, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the restaurant is not valid"},
			{Err: domain.ErrRestaurantNotFound, Status: http.StatusNotFound, Code: "restaurant_not_found", Detail: "the organisation has no restaurant with this ID"},
			{Err: domain.ErrRestaurantNameTaken, Status: http.StatusConflict, Code: "restaurant_name_taken", Detail: "the organisation already has a restaurant with this name"},
			{Err: domain.ErrRestaurantVersionConflict, Status: http.StatusConflict, Code: "restaurant_version_conflict", Detail: "the restaurant changed since you read it; get it again and retry"},
		},
		// Organisation permissions: every member holds them through their
		// role in the organisation, an API key only when its scopes include
		// them. Platform roles grant nothing in an organisation.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the organisation's restaurants", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermWrite, Description: "Create, change and delete the organisation's restaurants", OrgRoles: []string{"owner", "admin", "member"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
