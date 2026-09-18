package usecase

import "context"

// RemoveEndpoint deletes one of the organisation orgID's endpoints.
//
// Delivery jobs already queued for it keep their endpoint ID, and the worker
// cancels them when the row is gone rather than retrying: removing an
// endpoint is how a restaurant revokes a leaked webhook URL, so it has to
// take effect at once and stay taken.
func (s *Service) RemoveEndpoint(ctx context.Context, orgID, id string) error {
	if _, err := memberID(ctx, orgID); err != nil {
		return err
	}
	if err := s.store.DeleteEndpoint(ctx, orgID, id); err != nil {
		return storeError("remove", err)
	}
	s.audit(ctx, ActionRemoved, id, nil)
	return nil
}
