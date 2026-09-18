package usecase

import (
	"context"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// UpdateRestaurantInput changes a restaurant. Version is the version the caller read.
type UpdateRestaurantInput struct {
	Version int64
	Changes domain.Changes
}

// UpdateRestaurant changes one of the organisation orgID's restaurants. It returns
// ErrRestaurantVersionConflict when in.Version is no longer current. An update
// that changes nothing returns the restaurant as it is.
func (s *Service) UpdateRestaurant(ctx context.Context, orgID, id string, in UpdateRestaurantInput) (domain.Restaurant, error) {
	if _, err := memberID(ctx, orgID); err != nil {
		return domain.Restaurant{}, err
	}
	var updated domain.Restaurant
	var changed []string
	err := s.store.InTx(ctx, func(tx Store) error {
		current, err := tx.SelectRestaurant(ctx, orgID, id, true)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return domain.ErrRestaurantVersionConflict
		}
		next, fields, err := current.Apply(in.Changes, s.clock())
		if err != nil {
			return err
		}
		if len(fields) == 0 {
			updated = current
			return nil
		}
		changed = fields
		updated, err = tx.UpdateRestaurant(ctx, next)
		return err
	})
	if err != nil {
		return domain.Restaurant{}, storeError("update", err)
	}
	if len(changed) > 0 {
		// Field names only: values may be personal data.
		s.audit(ctx, ActionUpdated, id, map[string]any{"fields": changed})
	}
	return updated, nil
}
