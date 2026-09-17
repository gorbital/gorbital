package repository

import (
	"context"

	"example.com/invoicing/internal/modules/invoices/domain"
)

const deleteInvoiceSQL = `DELETE FROM invoices WHERE id = $1 AND org_id = $2`

// DeleteInvoice removes one of orgID's invoices, or returns
// ErrInvoiceNotFound.
func (s *Store) DeleteInvoice(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteInvoiceSQL, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrInvoiceNotFound
	}
	return nil
}
