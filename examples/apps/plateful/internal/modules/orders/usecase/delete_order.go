package usecase

import "context"

// DeleteOrder removes one of the organisation orgID's orders.
func (s *Service) DeleteOrder(ctx context.Context, orgID, id string) error {
	if _, err := memberID(ctx, orgID); err != nil {
		return err
	}
	if err := s.store.DeleteOrder(ctx, orgID, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
