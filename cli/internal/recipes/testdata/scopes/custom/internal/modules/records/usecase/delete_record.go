package usecase

import "context"

// DeleteRecord removes one record when the policy lets the caller change
// it. It reads the record first, because the policy decides on the record
// and not on its ID.
func (s *Service) DeleteRecord(ctx context.Context, id string) error {
	record, err := s.store.SelectRecord(ctx, id, false)
	if err != nil {
		return storeError("delete", err)
	}
	if err := s.policy.CanWrite(ctx, record); err != nil {
		return storeError("delete", err)
	}
	if err := s.store.DeleteRecord(ctx, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
