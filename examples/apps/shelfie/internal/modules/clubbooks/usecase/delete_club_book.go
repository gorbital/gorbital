package usecase

import "context"

// DeleteClubBook removes one of the organisation orgID's club books.
func (s *Service) DeleteClubBook(ctx context.Context, orgID, id string) error {
	if _, err := memberID(ctx, orgID); err != nil {
		return err
	}
	if err := s.store.DeleteClubBook(ctx, orgID, id); err != nil {
		return storeError("delete", err)
	}
	s.audit(ctx, ActionDeleted, id, nil)
	return nil
}
