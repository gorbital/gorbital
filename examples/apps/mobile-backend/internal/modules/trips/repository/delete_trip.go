package repository

import (
	"context"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

const deleteTripSQL = `DELETE FROM trips WHERE id = $1 AND owner_id = $2`

// DeleteTrip removes owner's trip id, or returns domain.ErrTripNotFound.
func (s *Store) DeleteTrip(ctx context.Context, owner, id string) error {
	tag, err := s.db.Exec(ctx, deleteTripSQL, id, owner)
	switch {
	case err != nil:
		return err
	case tag.RowsAffected() == 0:
		return domain.ErrTripNotFound
	}
	return nil
}
