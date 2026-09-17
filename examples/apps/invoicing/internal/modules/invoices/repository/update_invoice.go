package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/invoicing/internal/modules/invoices/domain"
)

const updateInvoiceSQL = `
	UPDATE invoices
	SET number = $3, customer = $4, status = $5, note = $6, updated_at = $7, version = version + 1
	WHERE id = $1 AND org_id = $2 AND version = $8
	RETURNING ` + invoiceColumns

// UpdateInvoice saves invoice when the stored version is still invoice.Version and
// returns it with the next version. It returns ErrInvoiceVersionConflict when
// no row has that version (changed, deleted or not the organisation's), and
// ErrInvoiceNumberTaken.
func (s *Store) UpdateInvoice(ctx context.Context, invoice domain.Invoice) (domain.Invoice, error) {
	rows, err := s.db.Query(ctx, updateInvoiceSQL,
		invoice.ID, invoice.OrgID, invoice.Number, invoice.Customer, invoice.Status, invoice.Note, invoice.UpdatedAt, invoice.Version)
	if err != nil {
		return domain.Invoice{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanInvoice)
	if postgres.IsNoRows(err) {
		return domain.Invoice{}, domain.ErrInvoiceVersionConflict
	}
	return updated, constraintError(err)
}
