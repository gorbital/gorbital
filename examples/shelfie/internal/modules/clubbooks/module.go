// Package clubbooks is the club books module: club books that belong to an
// organisation, whose members reach them through their role, in four layers
// (domain, usecase, repository, delivery) with one file per operation in
// each. orb gen module wrote it; the code is yours to change.
package clubbooks

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/shelfie/internal/modules/clubbooks/delivery"
	"example.com/shelfie/internal/modules/clubbooks/domain"
	"example.com/shelfie/internal/modules/clubbooks/repository"
	"example.com/shelfie/internal/modules/clubbooks/usecase"
)

// Module returns the club books module. main.go adds it with every other
// module through modules.All; its routes need the organisations module
// (orgshttp.Module), which answers guard.OrgMember. Error codes and
// permission names are public API: add new ones, never change existing
// ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "clubbooks",
		// guard.OrgMember answers org_not_found for an organisation the caller
		// isn't a member of, and forbidden for a role without the permission.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidClubBook, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the club book is not valid"},
			{Err: domain.ErrClubBookNotFound, Status: http.StatusNotFound, Code: "club_book_not_found", Detail: "the organisation has no club book with this ID"},
			{Err: domain.ErrClubBookTitleTaken, Status: http.StatusConflict, Code: "club_book_title_taken", Detail: "the organisation already has a club book with this title"},
			{Err: domain.ErrClubBookVersionConflict, Status: http.StatusConflict, Code: "club_book_version_conflict", Detail: "the club book changed since you read it; get it again and retry"},
		},
		// Organisation permissions: every member holds them through their
		// role in the organisation, an API key only when its scopes include
		// them. Platform roles grant nothing in an organisation.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the organisation's club books", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermWrite, Description: "Create, change and delete the organisation's club books", OrgRoles: []string{"owner", "admin", "member"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
