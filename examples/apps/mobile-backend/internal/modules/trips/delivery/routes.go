// Package delivery is the trips module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/mobile-backend/internal/modules/trips/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start routes

// Register adds the trips routes to r. Every route needs a caller, so the
// app's authenticator has to have verified a token from the identity
// provider: the guards hold the permissions the provider put in it. The
// use cases then scope each operation to that caller, so the permission
// says what a traveller may do, never whose trips they do it to.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	trips := r.Group("/v1/trips", gorbital.Tags("Trips"))

	gorbital.Post(trips, "", h.addTrip,
		gorbital.Summary("Add a trip"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite),
		guard.RateLimit(60, time.Hour, guard.Named("trips_add")))
	gorbital.Get(trips, "", h.listTrips,
		gorbital.Summary("List your trips"),
		guard.Permission(usecase.PermRead))
	gorbital.Get(trips, "/{id}", h.getTrip,
		gorbital.Summary("Get one of your trips"),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermRead))
	gorbital.Delete(trips, "/{id}", h.deleteTrip,
		gorbital.Summary("Delete one of your trips"), gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermWrite))
}

// docs:end routes
