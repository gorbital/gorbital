// Package invoices is the invoices module: invoices that belong to an
// organisation, whose members reach them through their role, in four layers
// (domain, usecase, repository, delivery) with one file per operation in
// each. orb gen module wrote it; the code is yours to change.
package invoices

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/invoicing/internal/modules/invoices/delivery"
	"example.com/invoicing/internal/modules/invoices/domain"
	"example.com/invoicing/internal/modules/invoices/repository"
	"example.com/invoicing/internal/modules/invoices/usecase"
)

// Module returns the invoices module. main.go adds it with every other
// module through modules.All; its routes need the organisations module
// (orgshttp.Module), which answers guard.OrgMember. Error codes and
// permission names are public API: add new ones, never change existing
// ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "invoices",
		// guard.OrgMember answers org_not_found for an organisation the caller
		// isn't a member of, and forbidden for a role without the permission.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidInvoice, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the invoice is not valid"},
			{Err: domain.ErrInvoiceNotFound, Status: http.StatusNotFound, Code: "invoice_not_found", Detail: "the organisation has no invoice with this ID"},
			{Err: domain.ErrInvoiceNumberTaken, Status: http.StatusConflict, Code: "invoice_number_taken", Detail: "the organisation already has an invoice with this number"},
			{Err: domain.ErrInvoiceVersionConflict, Status: http.StatusConflict, Code: "invoice_version_conflict", Detail: "the invoice changed since you read it; get it again and retry"},
		},
		// Organisation permissions: every member holds them through their
		// role in the organisation, an API key only when its scopes include
		// them. Platform roles grant nothing in an organisation.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the organisation's invoices", OrgRoles: []string{"owner", "admin", "member"}},
			{Name: usecase.PermWrite, Description: "Create, change and delete the organisation's invoices", OrgRoles: []string{"owner", "admin", "member"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
