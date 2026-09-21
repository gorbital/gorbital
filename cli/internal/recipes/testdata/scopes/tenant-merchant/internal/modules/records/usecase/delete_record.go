package usecase

import "context"

// DeleteRecord removes one of the merchant merchantID's records.
func (s *Service) DeleteRecord(ctx context.Context, merchantID, id string) error {
	if _, err := memberID(ctx, merchantID); err != nil {
		return err
	}
	if err := s.store.DeleteRecord(ctx, merchantID, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
