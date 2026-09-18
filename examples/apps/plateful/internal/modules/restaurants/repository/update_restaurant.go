package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/restaurants/domain"
)

const updateRestaurantSQL = `
	UPDATE restaurants
	SET name = $3, address = $4, cuisine = $5, status = $6, updated_at = $7, version = version + 1
	WHERE id = $1 AND org_id = $2 AND version = $8
	RETURNING ` + restaurantColumns

// UpdateRestaurant saves restaurant when the stored version is still restaurant.Version and
// returns it with the next version. It returns ErrRestaurantVersionConflict when
// no row has that version (changed, deleted or not the organisation's), and
// ErrRestaurantNameTaken.
func (s *Store) UpdateRestaurant(ctx context.Context, restaurant domain.Restaurant) (domain.Restaurant, error) {
	rows, err := s.db.Query(ctx, updateRestaurantSQL,
		restaurant.ID, restaurant.OrgID, restaurant.Name, restaurant.Address, restaurant.Cuisine, restaurant.Status, restaurant.UpdatedAt, restaurant.Version)
	if err != nil {
		return domain.Restaurant{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanRestaurant)
	if postgres.IsNoRows(err) {
		return domain.Restaurant{}, domain.ErrRestaurantVersionConflict
	}
	return updated, constraintError(err)
}
