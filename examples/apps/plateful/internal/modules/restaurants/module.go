// Package restaurants is Plateful's restaurants module: the restaurant
// domain of an organisation, whose fields live on the organisation's own row
// in orgs. Four layers (domain, usecase, repository, delivery) with one file
// per operation in each.
//
// It is where the platform's three kinds of caller meet one table. A
// restaurant's own staff read and write their profile through
// guard.OrgMember; any signed-in customer browses the open restaurants
// through a platform permission the "user" role holds; and platform staff
// suspend one through a platform permission only they hold. The routes are
// what tells them apart (delivery/routes.go), and the rules about what each
// may see are in the use cases.
package restaurants

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/settings"

	"example.com/plateful/internal/modules/restaurants/delivery"
	"example.com/plateful/internal/modules/restaurants/domain"
	"example.com/plateful/internal/modules/restaurants/repository"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

// docs:start module

// Module returns the restaurants module. main.go adds it with every other
// module through modules.All; its organisation routes need the
// organisations module (orgshttp.Module), which answers guard.OrgMember.
// Error codes, permission names and setting keys are public API: add new
// ones, never change existing ones.
func Module() gorbital.Module {
	// Declared in Settings, before the stores exist, and used in Routes: no
	// lookup by key.
	var maxRadius *settings.Setting[int]
	return gorbital.Module{
		Name: "restaurants",
		// docs:start errors
		// guard.OrgMember answers org_not_found for an organisation the
		// caller isn't a member of, and forbidden for a role without the
		// permission, so neither is mapped here.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidRestaurant, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the restaurant is not valid"},
			{Err: domain.ErrRestaurantNotFound, Status: http.StatusNotFound, Code: "restaurant_not_found", Detail: "no restaurant you can see has this ID"},
			{Err: domain.ErrRestaurantNameTaken, Status: http.StatusConflict, Code: "restaurant_name_taken", Detail: "another restaurant on the platform is called this"},
			{Err: domain.ErrRestaurantVersionConflict, Status: http.StatusConflict, Code: "restaurant_version_conflict", Detail: "the profile changed since you read it; get it again and retry"},
			{Err: domain.ErrRestaurantSuspended, Status: http.StatusConflict, Code: "restaurant_suspended", Detail: "the restaurant is suspended; write to the platform's support"},
			{Err: domain.ErrRestaurantNotSuspended, Status: http.StatusConflict, Code: "restaurant_not_suspended", Detail: "the restaurant is not suspended"},
			{Err: domain.ErrSuspensionIsPlatformOnly, Status: http.StatusUnprocessableEntity, Code: "suspension_is_platform_only", Detail: "only platform staff suspend a restaurant"},
		},
		// docs:end errors
		// docs:start permissions
		// Two catalogues, and a permission belongs to exactly one of them.
		// OrgRoles are held inside an organisation, by a member with that
		// role, through guard.OrgMember. Roles are platform roles: "user" is
		// held by every signed-in account without a grant, which is how a
		// customer — a member of no organisation at all — is allowed to
		// browse; platform_admin and ops_viewer are granted by an operator
		// with go run ./cmd/api grant-role.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See your restaurant's profile", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermWrite, Description: "Change your restaurant's profile", OrgRoles: []string{"owner", "admin"}},
			{Name: usecase.PermBrowse, Description: "Browse the restaurants that are open", Roles: []string{"user"}},
			{Name: usecase.PermOversee, Description: "See every restaurant on the platform, whatever its status", Roles: []string{"platform_admin", "ops_viewer"}},
			{Name: usecase.PermSuspend, Description: "Suspend a restaurant and lift its suspension", Roles: []string{"platform_admin"}},
		},
		// docs:end permissions
		// docs:start settings
		Settings: func(r *settings.Registry) {
			maxRadius = settings.Int(r, "restaurants.max_delivery_radius_m", 10_000,
				settings.Describe("How far a restaurant may say it delivers, in metres. Saving a larger radius answers 422; restaurants already above it keep theirs until they next save."),
				settings.Group("restaurants"),
				settings.Range(500, 50_000),
				settings.ReasonRequired(),
			)
		},
		// docs:end settings
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger, maxRadius)
			delivery.Register(r, svc)
		},
	}
}

// docs:end module
