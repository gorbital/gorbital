package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/invoicing/internal/modules/invoices/domain"
)

const (
	selectInvoiceSQL = `SELECT ` + invoiceColumns + ` FROM invoices WHERE id = $1 AND org_id = $2`
	forUpdate        = ` FOR UPDATE`
)

// SelectInvoice returns one of orgID's invoices, or ErrInvoiceNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectInvoice(ctx context.Context, orgID, id string, lock bool) (domain.Invoice, error) {
	sql := selectInvoiceSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id, orgID)
	if err != nil {
		return domain.Invoice{}, err
	}
	invoice, err := pgx.CollectExactlyOneRow(rows, scanInvoice)
	if postgres.IsNoRows(err) {
		return domain.Invoice{}, domain.ErrInvoiceNotFound
	}
	return invoice, err
}
