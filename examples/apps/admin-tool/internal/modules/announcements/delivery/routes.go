// Package delivery is the announcements module's HTTP adapter: the route
// table in this file, and one file per operation with its input, output and
// handler.
package delivery

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/admin-tool/internal/modules/announcements/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start routes

// Register adds the announcements routes to r: publishing needs staff's
// permission, reading is public.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	announcements := r.Group("/v1/announcements", gorbital.Tags("Announcements"))

	gorbital.Post(announcements, "", h.publishAnnouncement,
		gorbital.Summary("Publish an announcement"), gorbital.Status(http.StatusCreated),
		guard.Permission(usecase.PermWrite),
		guard.RateLimit(20, time.Hour, guard.Named("announcements_publish")))
	gorbital.Get(announcements, "", h.listAnnouncements,
		gorbital.Summary("List active announcements"), guard.Public())
}

// docs:end routes
