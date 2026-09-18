package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// docs:start create-restaurant-sql

// createRestaurantSQL turns an organisation into a restaurant anyone can
// see. There is no INSERT: the row is the organisation's, and it has existed
// since the organisation was created. Saving the first profile fills its
// restaurant columns in and sets profile_created_at, and the WHERE clause
// makes that happen once — a second save of a first profile finds no row.
const createRestaurantSQL = `
	UPDATE orgs
	SET name = $2, address = $3, cuisine = $4, opens_minute = $5, closes_minute = $6,
	    delivery_radius_m = $7, status = $8, suspended_reason = $9, cover_image_id = $10,
	    profile_created_at = $11, updated_at = $11, version = version + 1
	WHERE id = $1 AND profile_created_at IS NULL AND deleted_at IS NULL
	RETURNING ` + restaurantColumns

// docs:end create-restaurant-sql

// CreateRestaurant saves the organisation's first restaurant profile. It
// returns ErrRestaurantNameTaken when another restaurant already uses the
// name, ignoring case, and ErrRestaurantVersionConflict when the
// organisation already has a profile: two first saves raced, and the loser
// reads the winner's.
func (s *Store) CreateRestaurant(ctx context.Context, r domain.Restaurant) (domain.Restaurant, error) {
	rows, err := s.db.Query(ctx, createRestaurantSQL,
		r.ID, r.Name, r.Address, r.Cuisine, r.OpensMinute, r.ClosesMinute,
		r.DeliveryRadiusM, r.Status, r.SuspendedReason, r.CoverImageID, r.CreatedAt)
	if err != nil {
		return domain.Restaurant{}, constraintError(err)
	}
	created, err := pgx.CollectExactlyOneRow(rows, scanRestaurant)
	if postgres.IsNoRows(err) {
		return domain.Restaurant{}, domain.ErrRestaurantVersionConflict
	}
	return created, constraintError(err)
}
