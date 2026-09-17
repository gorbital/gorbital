package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

// docs:start select-trip

const selectTripSQL = `SELECT ` + tripColumns + ` FROM trips WHERE id = $1 AND owner_id = $2`

// SelectTrip returns owner's trip id. A trip that exists but belongs to
// somebody else matches no row, so it is domain.ErrTripNotFound like an ID
// that was never used.
func (s *Store) SelectTrip(ctx context.Context, owner, id string) (domain.Trip, error) {
	rows, err := s.db.Query(ctx, selectTripSQL, id, owner)
	if err != nil {
		return domain.Trip{}, err
	}
	t, err := pgx.CollectExactlyOneRow(rows, scanTrip)
	if postgres.IsNoRows(err) {
		return domain.Trip{}, domain.ErrTripNotFound
	}
	return t, err
}

// docs:end select-trip
