package usecase

import "context"

// DeleteRecord removes one record. The route requires PermWrite.
func (s *Service) DeleteRecord(ctx context.Context, id string) error {
	if err := s.store.DeleteRecord(ctx, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
