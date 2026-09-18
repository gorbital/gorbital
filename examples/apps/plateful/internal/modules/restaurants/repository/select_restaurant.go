package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/restaurants/domain"
)

const (
	selectRestaurantSQL      = `SELECT ` + restaurantColumns + ` FROM restaurants WHERE id = $1`
	selectRestaurantByOrgSQL = `SELECT ` + restaurantColumns + ` FROM restaurants WHERE org_id = $1`
	forUpdate                = ` FOR UPDATE`
)

// SelectRestaurant returns one restaurant by ID, or ErrRestaurantNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectRestaurant(ctx context.Context, id string, lock bool) (domain.Restaurant, error) {
	return s.selectOne(ctx, selectRestaurantSQL, id, lock)
}

// SelectRestaurantByOrg returns the organisation's restaurant, or
// ErrRestaurantNotFound.
func (s *Store) SelectRestaurantByOrg(ctx context.Context, orgID string, lock bool) (domain.Restaurant, error) {
	return s.selectOne(ctx, selectRestaurantByOrgSQL, orgID, lock)
}

func (s *Store) selectOne(ctx context.Context, sql, key string, lock bool) (domain.Restaurant, error) {
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, key)
	if err != nil {
		return domain.Restaurant{}, err
	}
	r, err := pgx.CollectExactlyOneRow(rows, scanRestaurant)
	if postgres.IsNoRows(err) {
		return domain.Restaurant{}, domain.ErrRestaurantNotFound
	}
	return r, err
}
