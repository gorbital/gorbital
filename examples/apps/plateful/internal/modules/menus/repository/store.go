// Package repository stores the menus module's items in PostgreSQL with
// hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/menus/domain"
	"example.com/plateful/internal/modules/menus/usecase"
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

// itemColumns are the columns scanItem reads, in its order.
const itemColumns = `id, org_id, created_by, section, position, name, description, ` +
	`price_minor, currency, available, stock, dietary, photo_image_id, version, created_at, updated_at`

// scanItem reads one row. price_minor is a bigint scanned straight into an
// int64: the price never becomes a float on its way through this process,
// because a float cannot hold 0.10 exactly and the totals the orders module
// adds up from these prices have to be right to the penny.
func scanItem(row pgx.CollectableRow) (domain.Item, error) {
	var item domain.Item
	err := row.Scan(&item.ID, &item.OrgID, &item.CreatedBy, &item.Section, &item.Position,
		&item.Name, &item.Description, &item.PriceMinor, &item.Currency, &item.Available,
		&item.Stock, &item.Dietary, &item.PhotoImageID, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	if item.Dietary == nil {
		// A text[] of no elements scans as an empty slice, but a NULL would
		// scan as nil; the API always shows a list.
		item.Dietary = []string{}
	}
	return item, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok && constraint == "menu_items_org_name" {
		return domain.ErrItemNameTaken
	}
	return err
}
