package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/restaurants/domain"
)

const selectRestaurantSQL = `SELECT ` + restaurantColumns + ` FROM orgs WHERE id = $1 AND ` + isRestaurant

// SelectRestaurant returns the restaurant whose organisation is id, or
// ErrRestaurantNotFound when the organisation has no profile yet. lock locks
// the row until the transaction ends.
func (s *Store) SelectRestaurant(ctx context.Context, id string, lock bool) (domain.Restaurant, error) {
	sql := selectRestaurantSQL
	if lock {
		sql += ` FOR UPDATE`
	}
	rows, err := s.db.Query(ctx, sql, id)
	if err != nil {
		return domain.Restaurant{}, err
	}
	r, err := pgx.CollectExactlyOneRow(rows, scanRestaurant)
	if postgres.IsNoRows(err) {
		return domain.Restaurant{}, domain.ErrRestaurantNotFound
	}
	return r, err
}
