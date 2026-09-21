// Package records is the records module: records anyone can read,
// which only a caller with the write permission can change, in four layers
// (domain, usecase, repository, delivery) with one file per operation in
// each. orb gen module wrote it; the code is yours to change.
package records

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/app/internal/modules/records/delivery"
	"example.com/app/internal/modules/records/domain"
	"example.com/app/internal/modules/records/repository"
	"example.com/app/internal/modules/records/usecase"
)

// Module returns the records module. main.go adds it with every other
// module through modules.All. Error codes and permission names are public
// API: add new ones, never change existing ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "records",
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidRecord, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the record is not valid"},
			{Err: domain.ErrRecordNotFound, Status: http.StatusNotFound, Code: "record_not_found", Detail: "no record has this ID"},
			{Err: domain.ErrRecordTitleTaken, Status: http.StatusConflict, Code: "record_title_taken", Detail: "a record with this title already exists"},
			{Err: domain.ErrRecordVersionConflict, Status: http.StatusConflict, Code: "record_version_conflict", Detail: "the record changed since you read it; get it again and retry"},
		},
		// Reading needs no permission, because reading needs no sign-in.
		// Writing does: a public record is published by somebody.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermWrite, Description: "Create, change and delete records", Roles: []string{"user"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
