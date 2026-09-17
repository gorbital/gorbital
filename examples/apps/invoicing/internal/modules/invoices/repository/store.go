// Package repository stores the invoices module's invoices in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/invoicing/internal/modules/invoices/domain"
	"example.com/invoicing/internal/modules/invoices/usecase"
)

// Store implements usecase.Store. It runs on the pool, or on a transaction
// inside InTx.
type Store struct {
	db   postgres.DBTX
	pool *pgxpool.Pool // nil inside a transaction
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool, pool: pool}
}

// InTx runs fn with a store bound to one transaction. Inside a transaction,
// fn joins it.
func (s *Store) InTx(ctx context.Context, fn func(tx usecase.Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{db: tx})
	})
}

// invoiceColumns are the columns scanInvoice reads, in its order.
const invoiceColumns = `id, org_id, created_by, number, customer, status, note, version, created_at, updated_at`

func scanInvoice(row pgx.CollectableRow) (domain.Invoice, error) {
	var invoice domain.Invoice
	err := row.Scan(&invoice.ID, &invoice.OrgID, &invoice.CreatedBy, &invoice.Number, &invoice.Customer, &invoice.Status, &invoice.Note, &invoice.Version, &invoice.CreatedAt, &invoice.UpdatedAt)
	invoice.CreatedAt, invoice.UpdatedAt = invoice.CreatedAt.UTC(), invoice.UpdatedAt.UTC()
	return invoice, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok {
		switch constraint {
		case "invoices_org_number":
			return domain.ErrInvoiceNumberTaken
		}
	}
	return err
}
