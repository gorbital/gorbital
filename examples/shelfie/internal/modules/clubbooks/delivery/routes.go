// Package delivery is the club books module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/shelfie/internal/modules/clubbooks/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// Register adds the club books routes to r, under an organisation. Every
// route requires a signed-in member of the organisation in the path whose
// role grants the permission its guard names (guard.OrgMember): anyone else
// gets 404 org_not_found, as if the organisation didn't exist. The use cases
// reach only that organisation's club books.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	clubBooks := r.Group("/v1/orgs/{orgId}/club-books", gorbital.Tags("Club books"))

	gorbital.Post(clubBooks, "", h.createClubBook, gorbital.OperationID("clubbooks-create"),
		gorbital.Summary("Create a club book"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Get(clubBooks, "", h.listClubBooks, gorbital.OperationID("clubbooks-list"),
		gorbital.Summary("List the organisation's club books"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at`, `title` or `author`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermRead))
	gorbital.Get(clubBooks, "/{id}", h.getClubBook, gorbital.OperationID("clubbooks-get"),
		gorbital.Summary("Get a club book"),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermRead))
	gorbital.Patch(clubBooks, "/{id}", h.updateClubBook, gorbital.OperationID("clubbooks-update"),
		gorbital.Summary("Update a club book"),
		gorbital.Description("Send the fields to change and the `version` you read."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Delete(clubBooks, "/{id}", h.deleteClubBook, gorbital.OperationID("clubbooks-delete"),
		gorbital.Summary("Delete a club book"), gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermWrite))
}
