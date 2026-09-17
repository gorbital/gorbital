package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

const selectTripsSQL = `
	SELECT ` + tripColumns + ` FROM trips
	WHERE owner_id = $1
	ORDER BY created_at DESC, id DESC
	LIMIT $2`

// SelectTrips returns up to limit of owner's trips, newest first.
func (s *Store) SelectTrips(ctx context.Context, owner string, limit int) ([]domain.Trip, error) {
	rows, err := s.db.Query(ctx, selectTripsSQL, owner, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanTrip)
}
