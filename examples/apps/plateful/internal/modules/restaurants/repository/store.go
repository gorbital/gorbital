// Package repository stores the restaurants module's restaurants in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/restaurants/domain"
	"example.com/plateful/internal/modules/restaurants/usecase"
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

// restaurantColumns are the columns scanRestaurant reads, in its order.
const restaurantColumns = `id, org_id, created_by, name, address, cuisine, status, version, created_at, updated_at`

func scanRestaurant(row pgx.CollectableRow) (domain.Restaurant, error) {
	var restaurant domain.Restaurant
	err := row.Scan(&restaurant.ID, &restaurant.OrgID, &restaurant.CreatedBy, &restaurant.Name, &restaurant.Address, &restaurant.Cuisine, &restaurant.Status, &restaurant.Version, &restaurant.CreatedAt, &restaurant.UpdatedAt)
	restaurant.CreatedAt, restaurant.UpdatedAt = restaurant.CreatedAt.UTC(), restaurant.UpdatedAt.UTC()
	return restaurant, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok {
		switch constraint {
		case "restaurants_org_name":
			return domain.ErrRestaurantNameTaken
		}
	}
	return err
}
