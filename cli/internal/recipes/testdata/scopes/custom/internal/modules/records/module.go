// Package records is the records module: records whose access rule is
// this module's own, in policy.go, in four layers (domain, usecase,
// repository, delivery) with one file per operation in each. orb gen module
// wrote it with --scope custom, so it added no rule of its own: until
// policy.go is written, the module refuses every request and its tests
// fail. The code is yours to change.
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
			{Err: domain.ErrRecordNotFound, Status: http.StatusNotFound, Code: "record_not_found", Detail: "no record you may see has this ID"},
			{Err: domain.ErrRecordTitleTaken, Status: http.StatusConflict, Code: "record_title_taken", Detail: "a record with this title already exists"},
			{Err: domain.ErrRecordVersionConflict, Status: http.StatusConflict, Code: "record_version_conflict", Detail: "the record changed since you read it; get it again and retry"},
			// Until policy.go is written, every request is refused with
			// 501 rather than served: an unimplemented rule is not an
			// open one.
			{Err: gorbital.ErrNotImplemented, Status: http.StatusNotImplemented, Code: "not_implemented", Detail: "the records access policy isn't written yet"},
		},
		// The permission is the floor, not the rule: Policy decides who may
		// see and change one record (policy.go).
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "Ask for records; the policy decides which ones", Roles: []string{"user"}},
			{Name: usecase.PermWrite, Description: "Ask to change records; the policy decides which ones", Roles: []string{"user"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			// Policy is the module's access rule, in policy.go. The store
			// asks it which rows a list may return; the service asks it
			// about every record it returns or changes.
			policy := Policy{}
			svc := usecase.NewService(repository.NewStore(d.DB, policy), policy, d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
