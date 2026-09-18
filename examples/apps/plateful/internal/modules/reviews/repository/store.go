// Package repository stores the reviews module's reviews, and the rating
// they add up to, in PostgreSQL with hand-written SQL, one file per
// operation. The tables come from db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/reviews/domain"
	"example.com/plateful/internal/modules/reviews/usecase"
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

// reviewColumns are the columns scanReview reads, in its order.
const reviewColumns = `id, org_id, restaurant_id, order_id, customer_id, rating, comment, ` +
	`hidden, hidden_reason, version, created_at, updated_at`

func scanReview(row pgx.CollectableRow) (domain.Review, error) {
	var r domain.Review
	err := row.Scan(&r.ID, &r.OrgID, &r.RestaurantID, &r.OrderID, &r.CustomerID,
		&r.Rating, &r.Comment, &r.Hidden, &r.HiddenReason, &r.Version, &r.CreatedAt, &r.UpdatedAt)
	r.CreatedAt, r.UpdatedAt = r.CreatedAt.UTC(), r.UpdatedAt.UTC()
	return r, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok && constraint == "reviews_order_id_key" {
		// Two reviews of one order: either the customer sent the request
		// twice, or two of their devices raced. The table is what decides,
		// not a SELECT before the INSERT, which would have a window between
		// the two.
		return domain.ErrReviewExists
	}
	return err
}
