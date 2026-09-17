package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/invoicing/internal/modules/invoices/domain"
)

const insertInvoiceSQL = `
	INSERT INTO invoices (id, org_id, created_by, number, customer, status, note, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	RETURNING ` + invoiceColumns

// InsertInvoice stores a new invoice, or returns ErrInvoiceNumberTaken when the
// organisation already uses the value, ignoring case.
func (s *Store) InsertInvoice(ctx context.Context, invoice domain.Invoice) (domain.Invoice, error) {
	rows, err := s.db.Query(ctx, insertInvoiceSQL,
		invoice.ID, invoice.OrgID, invoice.CreatedBy, invoice.Number, invoice.Customer, invoice.Status, invoice.Note, invoice.Version, invoice.CreatedAt, invoice.UpdatedAt)
	if err == nil {
		invoice, err = pgx.CollectExactlyOneRow(rows, scanInvoice)
	}
	return invoice, constraintError(err)
}
