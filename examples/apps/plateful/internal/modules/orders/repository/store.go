// Package repository stores the orders module's orders in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/usecase"
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

// orderColumns are the columns scanOrder reads, in its order.
const orderColumns = `id, org_id, created_by, status, address, note, version, created_at, updated_at`

func scanOrder(row pgx.CollectableRow) (domain.Order, error) {
	var order domain.Order
	err := row.Scan(&order.ID, &order.OrgID, &order.CreatedBy, &order.Status, &order.Address, &order.Note, &order.Version, &order.CreatedAt, &order.UpdatedAt)
	order.CreatedAt, order.UpdatedAt = order.CreatedAt.UTC(), order.UpdatedAt.UTC()
	return order, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors. The table has none yet: translate new unique constraints
// here.
func constraintError(err error) error { return err }
