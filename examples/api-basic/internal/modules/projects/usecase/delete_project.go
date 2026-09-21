package usecase

import "context"

// DeleteProject removes one of the signed-in user's projects.
func (s *Service) DeleteProject(ctx context.Context, id string) error {
	owner, err := ownerID(ctx)
	if err != nil {
		return err
	}
	if err := s.store.DeleteProject(ctx, owner, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
