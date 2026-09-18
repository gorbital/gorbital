package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/restaurants/domain"
)

const updateRestaurantSQL = `
	UPDATE restaurants
	SET name = $2, address = $3, cuisine = $4, opens_minute = $5, closes_minute = $6,
	    delivery_radius_m = $7, status = $8, suspended_reason = $9, cover_image_id = $10,
	    updated_at = $11, version = version + 1
	WHERE id = $1 AND version = $12
	RETURNING ` + restaurantColumns

// UpdateRestaurant saves r when the stored version is still r.Version and
// returns it with the next version. It returns ErrRestaurantVersionConflict
// when no row has that version (changed or deleted).
func (s *Store) UpdateRestaurant(ctx context.Context, r domain.Restaurant) (domain.Restaurant, error) {
	rows, err := s.db.Query(ctx, updateRestaurantSQL,
		r.ID, r.Name, r.Address, r.Cuisine, r.OpensMinute, r.ClosesMinute,
		r.DeliveryRadiusM, r.Status, r.SuspendedReason, r.CoverImageID, r.UpdatedAt, r.Version)
	if err != nil {
		return domain.Restaurant{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanRestaurant)
	if postgres.IsNoRows(err) {
		return domain.Restaurant{}, domain.ErrRestaurantVersionConflict
	}
	return updated, constraintError(err)
}
