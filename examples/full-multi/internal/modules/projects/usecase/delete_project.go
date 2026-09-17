package usecase

import "context"

// DeleteProject removes one of the organisation orgID's projects.
func (s *Service) DeleteProject(ctx context.Context, orgID, id string) error {
	if _, err := memberID(ctx, orgID); err != nil {
		return err
	}
	if err := s.store.DeleteProject(ctx, orgID, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
