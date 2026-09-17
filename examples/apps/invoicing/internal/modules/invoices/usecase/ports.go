package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/invoicing/internal/modules/invoices/domain"
)

// ListQuery selects one page of an organisation's invoices.
type ListQuery struct {
	OrgID string
	// Status keeps invoices with this status; empty keeps all.
	Status domain.Status
	// Sort is one of the sortable fields: created_at, updated_at, number or customer.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last invoice's sort value and ID.
type Position struct {
	Time time.Time // when sorting by created_at or updated_at
	Text string    // when sorting by a text field
	ID   string
}

// Store reads and writes invoices; repository.Store implements it with SQL.
// Every method is limited to one organisation's invoices.
type Store interface {
	// InsertInvoice stores a new invoice, or returns ErrInvoiceNumberTaken.
	InsertInvoice(ctx context.Context, invoice domain.Invoice) (domain.Invoice, error)
	// SelectInvoice returns one of orgID's invoices, or ErrInvoiceNotFound.
	// lock locks the row until the transaction ends.
	SelectInvoice(ctx context.Context, orgID, id string, lock bool) (domain.Invoice, error)
	// SelectInvoices returns up to q.Limit invoices in q.Sort order, with the
	// ID breaking ties.
	SelectInvoices(ctx context.Context, q ListQuery) ([]domain.Invoice, error)
	// UpdateInvoice saves invoice when the stored version is still invoice.Version and
	// increments the version. It returns ErrInvoiceVersionConflict when the
	// version changed or the invoice is gone, and ErrInvoiceNumberTaken.
	UpdateInvoice(ctx context.Context, invoice domain.Invoice) (domain.Invoice, error)
	// DeleteInvoice removes one of orgID's invoices, or returns
	// ErrInvoiceNotFound.
	DeleteInvoice(ctx context.Context, orgID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
