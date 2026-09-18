// Package delivery is the projects module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/projects/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// Register adds the projects routes to r, under an organisation. Every
// route requires a signed-in member of the organisation in the path whose
// role grants the permission its guard names (guard.OrgMember): anyone else
// gets 404 org_not_found, as if the organisation didn't exist. The use cases
// reach only that organisation's projects.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	projects := r.Group("/v1/orgs/{orgId}/projects", gorbital.Tags("Projects"))

	gorbital.Post(projects, "", h.createProject, gorbital.OperationID("projects-create"),
		gorbital.Summary("Create a project"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Get(projects, "", h.listProjects, gorbital.OperationID("projects-list"),
		gorbital.Summary("List the organisation's projects"),
		gorbital.Description("Newest first unless `sort` says otherwise; sort by one of `created_at`, `updated_at` or `name`. Paginate with `cursor`."),
		gorbital.Errors(http.StatusBadRequest, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermRead))
	gorbital.Get(projects, "/{id}", h.getProject, gorbital.OperationID("projects-get"),
		gorbital.Summary("Get a project"),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermRead))
	gorbital.Patch(projects, "/{id}", h.updateProject, gorbital.OperationID("projects-update"),
		gorbital.Summary("Update a project"),
		gorbital.Description("Send the fields to change and the `version` you read."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.OrgMember(usecase.PermWrite))
	gorbital.Delete(projects, "/{id}", h.deleteProject, gorbital.OperationID("projects-delete"),
		gorbital.Summary("Delete a project"), gorbital.Status(http.StatusNoContent),
		gorbital.Errors(http.StatusNotFound),
		guard.OrgMember(usecase.PermWrite))
}
