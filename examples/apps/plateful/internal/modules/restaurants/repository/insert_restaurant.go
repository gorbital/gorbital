package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/restaurants/domain"
)

const insertRestaurantSQL = `
	INSERT INTO restaurants (id, org_id, created_by, name, address, cuisine, status, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	RETURNING ` + restaurantColumns

// InsertRestaurant stores a new restaurant, or returns ErrRestaurantNameTaken when the
// organisation already uses the value, ignoring case.
func (s *Store) InsertRestaurant(ctx context.Context, restaurant domain.Restaurant) (domain.Restaurant, error) {
	rows, err := s.db.Query(ctx, insertRestaurantSQL,
		restaurant.ID, restaurant.OrgID, restaurant.CreatedBy, restaurant.Name, restaurant.Address, restaurant.Cuisine, restaurant.Status, restaurant.Version, restaurant.CreatedAt, restaurant.UpdatedAt)
	if err == nil {
		restaurant, err = pgx.CollectExactlyOneRow(rows, scanRestaurant)
	}
	return restaurant, constraintError(err)
}
