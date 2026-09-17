package usecase

import (
	"context"

	"example.com/invoicing/internal/modules/invoices/domain"
)

// GetInvoice returns one of the organisation orgID's invoices.
func (s *Service) GetInvoice(ctx context.Context, orgID, id string) (domain.Invoice, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Invoice{}, err
	}
	invoice, err := s.store.SelectInvoice(ctx, orgID, id, false)
	if err != nil {
		return domain.Invoice{}, storeError("get", err)
	}
	return invoice, nil
}
