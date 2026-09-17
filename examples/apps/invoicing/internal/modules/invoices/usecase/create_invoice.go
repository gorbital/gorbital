package usecase

import (
	"context"

	"example.com/invoicing/internal/modules/invoices/domain"
)

// CreateInvoice adds an invoice to the organisation orgID, created by the member
// acting in it.
func (s *Service) CreateInvoice(ctx context.Context, orgID string, f domain.InvoiceFields) (domain.Invoice, error) {
	member, err := memberID(ctx, orgID)
	if err != nil {
		return domain.Invoice{}, err
	}
	invoice, err := domain.NewInvoice(s.newID(), orgID, member, f, s.clock())
	if err != nil {
		return domain.Invoice{}, err
	}
	created, err := s.store.InsertInvoice(ctx, invoice)
	if err != nil {
		return domain.Invoice{}, storeError("create", err)
	}
	s.audit(ctx, ActionCreated, created.ID, nil)
	return created, nil
}
