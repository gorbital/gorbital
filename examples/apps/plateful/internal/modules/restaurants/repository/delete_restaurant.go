package repository

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
)

const deleteRestaurantSQL = `DELETE FROM restaurants WHERE id = $1 AND org_id = $2`

// DeleteRestaurant removes one of orgID's restaurants, or returns
// ErrRestaurantNotFound.
func (s *Store) DeleteRestaurant(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteRestaurantSQL, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRestaurantNotFound
	}
	return nil
}
