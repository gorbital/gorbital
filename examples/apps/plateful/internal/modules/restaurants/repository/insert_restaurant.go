package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/restaurants/domain"
)

const insertRestaurantSQL = `
	INSERT INTO restaurants (id, org_id, created_by, name, address, cuisine, opens_minute, closes_minute,
	                         delivery_radius_m, status, suspended_reason, cover_image_id, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	RETURNING ` + restaurantColumns

// InsertRestaurant stores a new restaurant, or returns
// ErrRestaurantNameTaken when another restaurant already uses the name,
// ignoring case.
func (s *Store) InsertRestaurant(ctx context.Context, r domain.Restaurant) (domain.Restaurant, error) {
	rows, err := s.db.Query(ctx, insertRestaurantSQL,
		r.ID, r.OrgID, r.CreatedBy, r.Name, r.Address, r.Cuisine, r.OpensMinute, r.ClosesMinute,
		r.DeliveryRadiusM, r.Status, r.SuspendedReason, r.CoverImageID, r.Version, r.CreatedAt, r.UpdatedAt)
	if err == nil {
		r, err = pgx.CollectExactlyOneRow(rows, scanRestaurant)
	}
	return r, constraintError(err)
}
