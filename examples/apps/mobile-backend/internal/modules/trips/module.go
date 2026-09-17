// Package trips is the mobile backend's trips module: the journeys one
// traveller keeps, in four layers (domain, usecase, repository, delivery)
// with one file per operation in each.
package trips

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/mobile-backend/internal/modules/trips/delivery"
	"example.com/mobile-backend/internal/modules/trips/domain"
	"example.com/mobile-backend/internal/modules/trips/repository"
	"example.com/mobile-backend/internal/modules/trips/usecase"
)

// docs:start module

// Module returns the trips module. main.go adds it with every other module
// through modules.All. Error codes and permission names are public API:
// add new ones, never change existing ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "trips",
		Errors: []httpx.Mapping{
			{Err: domain.ErrDestinationRequired, Status: http.StatusUnprocessableEntity, Code: "destination_required", Detail: "a trip needs a destination of up to 120 characters"},
			{Err: domain.ErrNotesTooLong, Status: http.StatusUnprocessableEntity, Code: "notes_too_long", Detail: "the notes are longer than 2000 characters"},
			{Err: domain.ErrTripNotFound, Status: http.StatusNotFound, Code: "trip_not_found", Detail: "you have no trip with this ID"},
		},
		// docs:start permissions
		// No Roles: this app has no roles to grant them. A caller holds
		// these because the identity provider put them in the token's
		// permissions claim; declaring them here documents them and lets
		// the dev console and the OpenAPI document list them.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See your own trips"},
			{Name: usecase.PermWrite, Description: "Add and delete your own trips"},
		},
		// docs:end permissions
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}

// docs:end module
