// Package repository stores the couriers module's couriers in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
//
// Every query here is written without an organisation, because the table has
// no org_id: a courier row is the platform's, not a tenant's. There is
// nothing to add to a WHERE clause and nothing for a row-level-security
// policy to compare, so the only thing keeping one courier out of another's
// profile is the user_id the use case passes in.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/couriers/domain"
	"example.com/plateful/internal/modules/couriers/usecase"
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

// courierColumns are the columns scanCourier reads, in its order. The names
// are a contract with the orders module, which reads and writes user_id,
// available and active_order_id on this table with its own SQL.
const courierColumns = `id, user_id, display_name, vehicle, available, active_order_id, ` +
	`version, created_at, updated_at`

func scanCourier(row pgx.CollectableRow) (domain.Courier, error) {
	var c domain.Courier
	err := row.Scan(&c.ID, &c.UserID, &c.DisplayName, &c.Vehicle, &c.Available,
		&c.ActiveOrderID, &c.Version, &c.CreatedAt, &c.UpdatedAt)
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	return c, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok && constraint == "couriers_user_id_key" {
		// One profile per sign-in account: the second registration, and the
		// loser of two racing first ones, both land here.
		return domain.ErrCourierAlreadyRegistered
	}
	return err
}
