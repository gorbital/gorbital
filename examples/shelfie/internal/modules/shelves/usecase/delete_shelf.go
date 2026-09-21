package usecase

import "context"

// DeleteShelf removes one of the signed-in user's shelves.
func (s *Service) DeleteShelf(ctx context.Context, id string) error {
	owner, err := ownerID(ctx)
	if err != nil {
		return err
	}
	if err := s.store.DeleteShelf(ctx, owner, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
