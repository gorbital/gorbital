package usecase

import "context"

// DeleteInvoice removes one of the organisation orgID's invoices.
func (s *Service) DeleteInvoice(ctx context.Context, orgID, id string) error {
	if _, err := memberID(ctx, orgID); err != nil {
		return err
	}
	if err := s.store.DeleteInvoice(ctx, orgID, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
