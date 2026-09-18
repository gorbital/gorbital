package usecase

import "context"

// DeleteRestaurant removes one of the organisation orgID's restaurants.
func (s *Service) DeleteRestaurant(ctx context.Context, orgID, id string) error {
	if _, err := memberID(ctx, orgID); err != nil {
		return err
	}
	if err := s.store.DeleteRestaurant(ctx, orgID, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
