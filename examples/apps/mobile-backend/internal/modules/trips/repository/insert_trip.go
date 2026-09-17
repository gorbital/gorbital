package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

const insertTripSQL = `
	INSERT INTO trips (id, owner_id, destination, notes, created_at)
	VALUES ($1, $2, $3, $4, $5)
	RETURNING ` + tripColumns

// InsertTrip stores a new trip.
func (s *Store) InsertTrip(ctx context.Context, t domain.Trip) (domain.Trip, error) {
	rows, err := s.db.Query(ctx, insertTripSQL, t.ID, t.OwnerID, t.Destination, t.Notes, t.CreatedAt)
	if err != nil {
		return domain.Trip{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanTrip)
}
