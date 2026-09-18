package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/orders/domain"
)

// ListQuery selects one page of an organisation's orders.
type ListQuery struct {
	OrgID string
	// Status keeps orders with this status; empty keeps all.
	Status domain.Status
	// Sort is one of the sortable fields: created_at, updated_at or address.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last order's sort value and ID.
type Position struct {
	Time time.Time // when sorting by created_at or updated_at
	Text string    // when sorting by a text field
	ID   string
}

// Store reads and writes orders; repository.Store implements it with SQL.
// Every method is limited to one organisation's orders.
type Store interface {
	// InsertOrder stores a new order.
	InsertOrder(ctx context.Context, order domain.Order) (domain.Order, error)
	// SelectOrder returns one of orgID's orders, or ErrOrderNotFound.
	// lock locks the row until the transaction ends.
	SelectOrder(ctx context.Context, orgID, id string, lock bool) (domain.Order, error)
	// SelectOrders returns up to q.Limit orders in q.Sort order, with the
	// ID breaking ties.
	SelectOrders(ctx context.Context, q ListQuery) ([]domain.Order, error)
	// UpdateOrder saves order when the stored version is still order.Version and
	// increments the version. It returns ErrOrderVersionConflict when the
	// version changed or the order is gone.
	UpdateOrder(ctx context.Context, order domain.Order) (domain.Order, error)
	// DeleteOrder removes one of orgID's orders, or returns
	// ErrOrderNotFound.
	DeleteOrder(ctx context.Context, orgID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
