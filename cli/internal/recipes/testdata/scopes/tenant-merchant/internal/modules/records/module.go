// Package records is the records module: records that belong to a
// merchant, whose members reach them through their role, in four layers
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
// module through modules.All; its routes need the merchants module
// (gorbital.WithScope), which answers guard.Scope. Error codes and
// permission names are public API: add new ones, never change existing
// ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "records",
		// guard.Scope answers merchant_not_found for a merchant the caller
		// isn't a member of, and forbidden for a role without the permission.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidRecord, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the record is not valid"},
			{Err: domain.ErrRecordNotFound, Status: http.StatusNotFound, Code: "record_not_found", Detail: "the merchant has no record with this ID"},
			{Err: domain.ErrRecordTitleTaken, Status: http.StatusConflict, Code: "record_title_taken", Detail: "the merchant already has a record with this title"},
			{Err: domain.ErrRecordVersionConflict, Status: http.StatusConflict, Code: "record_version_conflict", Detail: "the record changed since you read it; get it again and retry"},
		},
		// Merchant permissions: every member holds them through their
		// role in the merchant, an API key only when its scopes include
		// them. Platform roles grant nothing in a merchant.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the merchant's records", ScopeRoles: []string{"owner", "manager", "courier"}},
			{Name: usecase.PermWrite, Description: "Create, change and delete the merchant's records", ScopeRoles: []string{"owner", "manager", "courier"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
