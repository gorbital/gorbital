package usecase

import "context"

// DeleteRecord removes one of the signed-in user's records.
func (s *Service) DeleteRecord(ctx context.Context, id string) error {
	owner, err := ownerID(ctx)
	if err != nil {
		return err
	}
	if err := s.store.DeleteRecord(ctx, owner, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
