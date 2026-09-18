package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/restaurants/domain"
)

const (
	selectRestaurantSQL = `SELECT ` + restaurantColumns + ` FROM restaurants WHERE id = $1 AND org_id = $2`
	forUpdate           = ` FOR UPDATE`
)

// SelectRestaurant returns one of orgID's restaurants, or ErrRestaurantNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectRestaurant(ctx context.Context, orgID, id string, lock bool) (domain.Restaurant, error) {
	sql := selectRestaurantSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id, orgID)
	if err != nil {
		return domain.Restaurant{}, err
	}
	restaurant, err := pgx.CollectExactlyOneRow(rows, scanRestaurant)
	if postgres.IsNoRows(err) {
		return domain.Restaurant{}, domain.ErrRestaurantNotFound
	}
	return restaurant, err
}
