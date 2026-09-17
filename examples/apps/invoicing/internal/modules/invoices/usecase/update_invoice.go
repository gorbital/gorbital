package usecase

import (
	"context"

	"example.com/invoicing/internal/modules/invoices/domain"
)

// UpdateInvoiceInput changes an invoice. Version is the version the caller read.
type UpdateInvoiceInput struct {
	Version int64
	Changes domain.Changes
}

// UpdateInvoice changes one of the organisation orgID's invoices. It returns
// ErrInvoiceVersionConflict when in.Version is no longer current. An update
// that changes nothing returns the invoice as it is.
func (s *Service) UpdateInvoice(ctx context.Context, orgID, id string, in UpdateInvoiceInput) (domain.Invoice, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Invoice{}, err
	}
	var updated domain.Invoice
	var changed []string
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectInvoice(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrInvoiceVersionConflict
		}
		next, fields, err := current.Apply(in.Changes, s.clock())
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			updated = current
			return nil
		}
		changed = fields
		updated, err = tx.UpdateInvoice(ctx, next)
		return err
	})
	if err != nil {
		return domain.Invoice{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: values may be personal data.
		s.audit(ctx, ActionUpdated, id, map[string]any{"fields": changed})
	}
	return updated, nil
}
