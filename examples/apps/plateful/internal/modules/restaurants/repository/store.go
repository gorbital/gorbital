// Package repository stores the restaurants module's restaurants in
// PostgreSQL with hand-written SQL, one file per operation. The table comes
// from db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/restaurants/domain"
	"example.com/plateful/internal/modules/restaurants/usecase"
)

// docs:start restaurant-store

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

// docs:end restaurant-store

// restaurantColumns are the columns scanRestaurant reads, in its order.
const restaurantColumns = `id, org_id, created_by, name, address, cuisine, opens_minute, closes_minute, ` +
	`delivery_radius_m, status, suspended_reason, cover_image_id, version, created_at, updated_at`

func scanRestaurant(row pgx.CollectableRow) (domain.Restaurant, error) {
	var r domain.Restaurant
	err := row.Scan(&r.ID, &r.OrgID, &r.CreatedBy, &r.Name, &r.Address, &r.Cuisine,
		&r.OpensMinute, &r.ClosesMinute, &r.DeliveryRadiusM, &r.Status, &r.SuspendedReason, &r.CoverImageID,
		&r.Version, &r.CreatedAt, &r.UpdatedAt)
	r.CreatedAt, r.UpdatedAt = r.CreatedAt.UTC(), r.UpdatedAt.UTC()
	return r, err
}

// docs:start restaurant-constraint-error

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok {
		switch constraint {
		case "restaurants_name":
			return domain.ErrRestaurantNameTaken
		case "restaurants_org_id_key":
			// The organisation already has a restaurant: two saves of a
			// first profile raced, and the loser reads the winner's row.
			return domain.ErrRestaurantVersionConflict
		}
	}
	return err
}

// docs:end restaurant-constraint-error
