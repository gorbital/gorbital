// Package shelves is the shelves module: shelves that belong to the
// signed-in user, in four layers (domain, usecase, repository, delivery)
// with one file per operation in each. orb gen module wrote it; the code is
// yours to change.
package shelves

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/shelfie/internal/modules/shelves/delivery"
	"example.com/shelfie/internal/modules/shelves/domain"
	"example.com/shelfie/internal/modules/shelves/repository"
	"example.com/shelfie/internal/modules/shelves/usecase"
)

// Module returns the shelves module. main.go adds it with every other
// module through modules.All. Error codes and permission names are public
// API: add new ones, never change existing ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "shelves",
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidShelf, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the shelf is not valid"},
			{Err: domain.ErrShelfNotFound, Status: http.StatusNotFound, Code: "shelf_not_found", Detail: "no shelf of yours has this ID"},
			{Err: domain.ErrShelfNameTaken, Status: http.StatusConflict, Code: "shelf_name_taken", Detail: "you already have a shelf with this name"},
			{Err: domain.ErrShelfVersionConflict, Status: http.StatusConflict, Code: "shelf_version_conflict", Detail: "the shelf changed since you read it; get it again and retry"},
		},
		// Every user holds these through the user role; an API key only when
		// its scopes include them.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See your shelves", Roles: []string{"user"}},
			{Name: usecase.PermWrite, Description: "Create, change and delete your shelves", Roles: []string{"user"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
