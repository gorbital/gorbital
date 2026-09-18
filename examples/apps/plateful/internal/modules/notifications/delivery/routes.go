// Package delivery is the notifications module's HTTP adapter: the route
// table in this file, and one file per operation with its input, output and
// handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/notifications/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start endpoint-routes

// Register adds the notification endpoint routes to r, under an
// organisation. Every route requires a signed-in member of the organisation
// in the path whose role grants the permission its guard names
// (guard.OrgMember): anyone else gets 404 org_not_found, as if the
// organisation didn't exist.
//
// There is no update route on purpose. Changing an endpoint's URL is
// replacing one credential with another, and a PATCH that took a url field
// would mean a request body carrying a secret, an old secret sitting in the
// row until it is overwritten and a version conflict to reason about. Delete
// and register again is one less thing to get wrong, and it is what a
// restaurant does anyway when a webhook leaks.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	endpoints := r.Group("/v1/orgs/{orgId}/notification-endpoints", gorbital.Tags("Notifications"))

	gorbital.Post(endpoints, "", h.registerEndpoint, gorbital.OperationID("notifications-endpoints-register"),
		gorbital.Summary("Register a notification endpoint"),
		gorbital.Description("Tells Plateful where to post this restaurant's alerts. `url` is a Slack-style incoming webhook; it is stored, never returned and never logged. It must be an `https` URL that doesn't point inside a private network."),
		gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Get(endpoints, "", h.listEndpoints, gorbital.OperationID("notifications-endpoints-list"),
		gorbital.Summary("List the restaurant's notification endpoints"),
		gorbital.Description("Each endpoint comes back with the host it posts to and how the last delivery went. The URL itself is never returned."),
		guard.OrgMember(usecase.PermRead))
	gorbital.Delete(endpoints, "/{id}", h.removeEndpoint, gorbital.OperationID("notifications-endpoints-remove"),
		gorbital.Summary("Remove a notification endpoint"),
		gorbital.Description("Stops alerts at once, including deliveries already queued. This is how a leaked webhook URL is revoked."),
		gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermWrite))
}

// docs:end endpoint-routes
